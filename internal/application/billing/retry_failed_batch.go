package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrChargeBatchNotFound  = errors.New("billing charge batch not found")
	ErrChargeBatchNotFailed = errors.New("billing charge batch is not failed")
)

type FailedBatchRetryService struct {
	charge ports.BillingChargeClient
	ledger ports.AdminUsageLedger
	clock  ports.Clock
}

func NewFailedBatchRetryService(charge ports.BillingChargeClient, usageLedger ports.AdminUsageLedger, clock ports.Clock) (*FailedBatchRetryService, error) {
	if charge == nil || usageLedger == nil || clock == nil {
		return nil, fmt.Errorf("%w: retry dependency", ErrInvalidBillingInput)
	}
	return &FailedBatchRetryService{charge: charge, ledger: usageLedger, clock: clock}, nil
}

func (s *FailedBatchRetryService) RetryFailedBatch(ctx context.Context, batchID string, audit domain.AuditContext) (domain.BillingChargeBatch, error) {
	snapshot, err := s.ledger.LoadChargeBatchByID(ctx, batchID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return domain.BillingChargeBatch{}, ErrChargeBatchNotFound
		}
		return domain.BillingChargeBatch{}, ErrBillingStoreUnavailable
	}
	if snapshot.Batch.ID != batchID {
		return domain.BillingChargeBatch{}, ErrBillingStoreContractViolation
	}
	if snapshot.Batch.Status != domain.BillingChargeStatusFailed {
		return domain.BillingChargeBatch{}, ErrChargeBatchNotFailed
	}
	if audit.Action != domain.AuditActionBillingChargeRetry || audit.AdminSubject != "admin_token" || audit.EntityType != "billing_charge_batch" || audit.EntityID != batchID || audit.RequestID == "" || audit.ID == "" {
		return domain.BillingChargeBatch{}, ErrBillingStoreContractViolation
	}
	input := AutoChargeInput{UserID: snapshot.Batch.UserID, BillingSubjectUserID: snapshot.Batch.BillingSubjectUserID, Currency: snapshot.Batch.Currency}
	if err := ValidateChargeSnapshot(input, snapshot); err != nil {
		return domain.BillingChargeBatch{}, ErrBillingStoreContractViolation
	}

	now := s.clock.Now().UTC()
	if now.IsZero() || now.Location() != time.UTC {
		return domain.BillingChargeBatch{}, ErrBillingStoreContractViolation
	}

	// Persist who initiated the external financial side effect before calling
	// Billing. This audit survives invalid Billing responses and later local
	// reconciliation failures.
	attemptAudit := retryPhaseAudit(audit, "attempt", now)
	attemptAudit.BeforeState = billingRetryAuditState(snapshot.Batch)
	attemptAudit.AfterState = billingRetryAuditState(snapshot.Batch)
	if err := s.ledger.RecordChargeRetryAttemptWithAudit(ctx, snapshot, attemptAudit); err != nil {
		switch {
		case errors.Is(err, ports.ErrNotFound):
			return domain.BillingChargeBatch{}, ErrChargeBatchNotFound
		case errors.Is(err, ports.ErrAdminStateConflict):
			return domain.BillingChargeBatch{}, ErrChargeBatchNotFailed
		default:
			return domain.BillingChargeBatch{}, ErrBillingStoreUnavailable
		}
	}

	chargeResult, err := s.charge.Charge(ctx, ports.BillingChargeRequest{
		RequestID:    snapshot.Batch.ID,
		UserID:       snapshot.Batch.BillingSubjectUserID,
		Model:        snapshot.Batch.BillingModel,
		InputTokens:  snapshot.Batch.InputTokens,
		OutputTokens: snapshot.Batch.OutputTokens,
		AmountCents:  snapshot.Batch.AmountCents,
		Currency:     snapshot.Batch.Currency,
	})
	if err != nil {
		failed := snapshot.Batch
		failed.Status = domain.BillingChargeStatusFailed
		failed.BillingErrorCode = "billing_unavailable"
		failed.FailedAt = &now
		failed.UpdatedAt = now

		outcomeAudit := retryPhaseAudit(audit, "failed", now)
		outcomeAudit.BeforeState = billingRetryAuditState(snapshot.Batch)
		outcomeAudit.AfterState = billingRetryAuditState(failed)
		if markErr := s.ledger.MarkChargeRetryFailedWithAudit(ctx, snapshot.Batch.ID, domain.BillingChargeStatusFailed, "billing_unavailable", now, outcomeAudit); markErr != nil {
			return domain.BillingChargeBatch{}, ErrChargeReconciliationRequired
		}
		return failed, ErrBillingUnavailable
	}
	if err := validateBillingChargeResult(chargeResult); err != nil {
		// The pre-call attempt audit is already durable. The externally observed
		// result is unsafe to reconcile automatically.
		return domain.BillingChargeBatch{}, ErrChargeReconciliationRequired
	}

	succeeded := snapshot.Batch
	succeeded.Status = domain.BillingChargeStatusSucceeded
	succeeded.BillingResponseBalanceCents = chargeResult.BalanceCents
	succeeded.BillingErrorCode = ""
	succeeded.ChargedAt = &now
	succeeded.FailedAt = nil
	succeeded.UpdatedAt = now

	outcomeAudit := retryPhaseAudit(audit, "succeeded", now)
	outcomeAudit.BeforeState = billingRetryAuditState(snapshot.Batch)
	outcomeAudit.AfterState = billingRetryAuditState(succeeded)
	success := ports.UsageChargeSuccess{
		BatchID:             snapshot.Batch.ID,
		BillingBalanceCents: chargeResult.BalanceCents,
		ChargedAt:           now,
		Allocations:         snapshot.Allocations,
		ExpectedRecords:     snapshot.ExpectedRecords,
	}
	if err := s.ledger.ApplyChargeRetrySuccessWithAudit(ctx, success, outcomeAudit); err != nil {
		// The attempt audit was committed before the external call, so the
		// initiator and request ID remain durable even when reconciliation fails.
		return domain.BillingChargeBatch{}, ErrChargeReconciliationRequired
	}
	return succeeded, nil
}

func retryPhaseAudit(base domain.AuditContext, phase string, at time.Time) domain.AuditContext {
	sum := sha256.Sum256([]byte(base.ID + "\x00" + phase))
	base.ID = "audit_" + hex.EncodeToString(sum[:])
	base.CreatedAt = at.UTC()
	return base
}

func billingRetryAuditState(batch domain.BillingChargeBatch) domain.AuditState {
	return domain.AuditState{
		"id":                             batch.ID,
		"user_id":                        batch.UserID,
		"billing_subject_user_id":        batch.BillingSubjectUserID,
		"provider_type":                  batch.ProviderType,
		"client_model":                   batch.ClientModel,
		"billing_model":                  batch.BillingModel,
		"input_tokens":                   batch.InputTokens,
		"output_tokens":                  batch.OutputTokens,
		"amount_cents":                   batch.AmountCents,
		"currency":                       batch.Currency,
		"billing_status":                 batch.Status,
		"billing_response_balance_cents": batch.BillingResponseBalanceCents,
		"billing_error_code":             batch.BillingErrorCode,
		"created_at":                     batch.CreatedAt,
		"charged_at":                     batch.ChargedAt,
		"failed_at":                      batch.FailedAt,
		"updated_at":                     batch.UpdatedAt,
	}
}
