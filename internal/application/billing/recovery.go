package billing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type preparedBatchProcessor interface {
	processPreparedBatch(
		context.Context,
		AutoChargeInput,
		ports.BillingChargeBatchSnapshot,
	) (AutoChargeResult, error)
}

type RecoveryCycleResult struct {
	DiscoveredBatchIDs []string
	ProcessedBatchIDs  []string
}

type RecoveryService struct {
	store     ports.BillingRecoveryStore
	processor preparedBatchProcessor
}

func NewRecoveryService(
	store ports.BillingRecoveryStore,
	processor preparedBatchProcessor,
) (*RecoveryService, error) {
	if store == nil || processor == nil {
		return nil, fmt.Errorf(
			"%w: billing recovery dependencies",
			ErrInvalidBillingInput,
		)
	}
	return &RecoveryService{store: store, processor: processor}, nil
}

func (s *RecoveryService) RunCycle(
	ctx context.Context,
	limit int,
) (RecoveryCycleResult, error) {
	var result RecoveryCycleResult
	if ctx == nil || s == nil || s.store == nil ||
		s.processor == nil || limit < 1 {
		return result, fmt.Errorf(
			"%w: billing recovery cycle",
			ErrInvalidBillingInput,
		)
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
	sort.Slice(snapshots, func(left int, right int) bool {
		leftBatch := snapshots[left].Batch
		rightBatch := snapshots[right].Batch
		if !leftBatch.CreatedAt.Equal(rightBatch.CreatedAt) {
			return leftBatch.CreatedAt.Before(rightBatch.CreatedAt)
		}
		return leftBatch.ID < rightBatch.ID
	})

	seen := make(map[string]struct{}, len(snapshots))
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
		if _, exists := seen[batch.ID]; exists {
			return result, ErrBillingStoreContractViolation
		}
		seen[batch.ID] = struct{}{}
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
		result.ProcessedBatchIDs = append(
			result.ProcessedBatchIDs,
			processed.ProcessedBatchIDs...,
		)
	}
	return result, cycleErr
}
