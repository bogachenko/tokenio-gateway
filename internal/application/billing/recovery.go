package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type preparedBatchProcessor interface {
	processPreparedBatch(context.Context, AutoChargeInput, ports.BillingChargeBatchSnapshot) (AutoChargeResult, error)
	processNewBatches(context.Context, AutoChargeInput, int) (AutoChargeResult, error)
}

type RecoveryCycleResult struct {
	DiscoveredBatchIDs []string
	ProcessedBatchIDs  []string
}

type RecoveryService struct {
	store     ports.BillingRecoveryStore
	processor preparedBatchProcessor
}

func NewRecoveryService(store ports.BillingRecoveryStore, processor preparedBatchProcessor) (*RecoveryService, error) {
	if store == nil || processor == nil {
		return nil, fmt.Errorf("%w: billing recovery dependencies", ErrInvalidBillingInput)
	}
	return &RecoveryService{store: store, processor: processor}, nil
}

func (s *RecoveryService) RunCycle(ctx context.Context, limit int) (RecoveryCycleResult, error) {
	var result RecoveryCycleResult
	if ctx == nil || s == nil || s.store == nil || s.processor == nil || limit < 1 {
		return result, fmt.Errorf("%w: billing recovery cycle", ErrInvalidBillingInput)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	snapshots, err := s.store.ListOpenChargeBatchesForRecovery(ctx, limit)
	if err != nil {
		return result, ErrBillingStoreUnavailable
	}
	if len(snapshots) > limit {
		return result, ErrBillingStoreContractViolation
	}
	snapshots = append([]ports.BillingChargeBatchSnapshot(nil), snapshots...)
	sort.Slice(snapshots, func(i, j int) bool {
		if !snapshots[i].Batch.CreatedAt.Equal(snapshots[j].Batch.CreatedAt) {
			return snapshots[i].Batch.CreatedAt.Before(snapshots[j].Batch.CreatedAt)
		}
		return snapshots[i].Batch.ID < snapshots[j].Batch.ID
	})

	seenDiscoveredBatchIDs := make(map[string]struct{}, limit)
	seenProcessedBatchIDs := make(map[string]struct{}, limit)
	var cycleErr error
	for _, snapshot := range snapshots {
		batch := snapshot.Batch
		if strings.TrimSpace(batch.ID) == "" ||
			strings.TrimSpace(batch.UserID) == "" ||
			strings.TrimSpace(batch.BillingSubjectUserID) == "" ||
			strings.TrimSpace(batch.Currency) == "" ||
			(batch.Status != domain.BillingChargeStatusPending &&
				batch.Status != domain.BillingChargeStatusFailed) {
			return result, ErrBillingStoreContractViolation
		}
		if _, exists := seenDiscoveredBatchIDs[batch.ID]; exists {
			return result, ErrBillingStoreContractViolation
		}
		seenDiscoveredBatchIDs[batch.ID] = struct{}{}
		result.DiscoveredBatchIDs = append(result.DiscoveredBatchIDs, batch.ID)

		processed, processErr := s.processor.processPreparedBatch(
			ctx,
			AutoChargeInput{
				UserID:               batch.UserID,
				BillingSubjectUserID: batch.BillingSubjectUserID,
				Currency:             batch.Currency,
			},
			snapshot,
		)
		if processErr != nil {
			cycleErr = errors.Join(cycleErr, processErr)
			continue
		}
		if err := appendProcessedBatchIDs(&result, seenProcessedBatchIDs, processed.ProcessedBatchIDs); err != nil {
			return result, err
		}
	}

	remaining := limit - len(snapshots)
	if remaining == 0 {
		return result, cycleErr
	}

	subjects, err := s.store.ListChargeableBillingSubjects(ctx, remaining)
	if err != nil {
		return result, errors.Join(cycleErr, ErrBillingStoreUnavailable)
	}
	if len(subjects) > remaining {
		return result, ErrBillingStoreContractViolation
	}
	subjects = append([]ports.BillingChargeSubject(nil), subjects...)
	sort.Slice(subjects, func(i, j int) bool {
		if !subjects[i].OldestChargeableAt.Equal(subjects[j].OldestChargeableAt) {
			return subjects[i].OldestChargeableAt.Before(subjects[j].OldestChargeableAt)
		}
		if subjects[i].UserID != subjects[j].UserID {
			return subjects[i].UserID < subjects[j].UserID
		}
		if subjects[i].BillingSubjectUserID != subjects[j].BillingSubjectUserID {
			return subjects[i].BillingSubjectUserID < subjects[j].BillingSubjectUserID
		}
		return subjects[i].Currency < subjects[j].Currency
	})

	seenSubjects := make(map[string]struct{}, len(subjects))
	for _, subject := range subjects {
		if remaining == 0 {
			break
		}
		if strings.TrimSpace(subject.UserID) == "" ||
			strings.TrimSpace(subject.BillingSubjectUserID) == "" ||
			strings.TrimSpace(subject.Currency) == "" ||
			subject.OldestChargeableAt.IsZero() ||
			subject.OldestChargeableAt.Location() != time.UTC {
			return result, ErrBillingStoreContractViolation
		}
		key := subject.UserID + "\x00" + subject.BillingSubjectUserID + "\x00" + subject.Currency
		if _, exists := seenSubjects[key]; exists {
			return result, ErrBillingStoreContractViolation
		}
		seenSubjects[key] = struct{}{}

		processed, processErr := s.processor.processNewBatches(
			ctx,
			AutoChargeInput{
				UserID:               subject.UserID,
				BillingSubjectUserID: subject.BillingSubjectUserID,
				Currency:             subject.Currency,
			},
			remaining,
		)
		if len(processed.ProcessedBatchIDs) > remaining {
			return result, ErrBillingStoreContractViolation
		}
		if err := appendProcessedBatchIDs(&result, seenProcessedBatchIDs, processed.ProcessedBatchIDs); err != nil {
			return result, err
		}
		remaining -= len(processed.ProcessedBatchIDs)
		if processErr != nil && !errors.Is(processErr, ErrChargeDeferred) {
			cycleErr = errors.Join(cycleErr, processErr)
		}
	}
	return result, cycleErr
}

func appendProcessedBatchIDs(result *RecoveryCycleResult, seen map[string]struct{}, batchIDs []string) error {
	for _, batchID := range batchIDs {
		if strings.TrimSpace(batchID) == "" {
			return ErrBillingStoreContractViolation
		}
		if _, exists := seen[batchID]; exists {
			return ErrBillingStoreContractViolation
		}
		seen[batchID] = struct{}{}
		result.ProcessedBatchIDs = append(result.ProcessedBatchIDs, batchID)
	}
	return nil
}
