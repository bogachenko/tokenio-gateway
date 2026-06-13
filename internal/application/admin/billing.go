package admin

import (
	"context"
	"errors"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func billingBatchState(batch domain.BillingChargeBatch) domain.AuditState {
	return domain.AuditState{
		"id": batch.ID, "user_id": batch.UserID,
		"billing_subject_user_id": batch.BillingSubjectUserID,
		"provider_type":           batch.ProviderType, "client_model": batch.ClientModel,
		"billing_model": batch.BillingModel, "input_tokens": batch.InputTokens,
		"output_tokens": batch.OutputTokens, "amount_cents": batch.AmountCents,
		"currency": batch.Currency, "billing_status": batch.Status,
		"billing_response_balance_cents": batch.BillingResponseBalanceCents,
		"billing_error_code":             batch.BillingErrorCode,
		"created_at":                     batch.CreatedAt, "charged_at": batch.ChargedAt,
		"failed_at": batch.FailedAt, "updated_at": batch.UpdatedAt,
	}
}

func (s *Service) ListBillingChargeBatches(ctx context.Context, input BillingBatchListInput) (ListResult[domain.BillingChargeBatch], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[domain.BillingChargeBatch]{}, err
	}
	if err := validateWindow(input.CreatedFrom, input.CreatedTo); err != nil {
		return ListResult[domain.BillingChargeBatch]{}, err
	}
	if input.ProviderType != "" && !validateProviderType(input.ProviderType) {
		return ListResult[domain.BillingChargeBatch]{}, ErrInvalidRequest
	}
	page, err := s.deps.Ledger.ListBillingChargeBatches(ctx, ports.BillingChargeBatchListFilter{UserID: input.UserID, ProviderType: input.ProviderType, ClientModel: input.ClientModel, Status: input.Status, CreatedFrom: input.CreatedFrom, CreatedTo: input.CreatedTo, Page: pageReq})
	if err != nil {
		return ListResult[domain.BillingChargeBatch]{}, mapStoreError(err)
	}
	return listResult(page, pageReq), nil
}

func (s *Service) GetBillingChargeBatch(ctx context.Context, id string) (ports.BillingChargeBatchSnapshot, error) {
	if !validBillingChargeID(id) {
		return ports.BillingChargeBatchSnapshot{}, ErrInvalidRequest
	}
	snapshot, err := s.deps.Ledger.LoadChargeBatchByID(ctx, id)
	if err != nil {
		return ports.BillingChargeBatchSnapshot{}, mapStoreError(err)
	}
	return snapshot, nil
}

func (s *Service) RetryFailedBillingChargeBatch(ctx context.Context, command CommandContext, id string) (domain.BillingChargeBatch, error) {
	if validateCommand(command) != nil || !validBillingChargeID(id) {
		return domain.BillingChargeBatch{}, ErrInvalidRequest
	}
	snapshot, err := s.deps.Ledger.LoadChargeBatchByID(ctx, id)
	if err != nil {
		return domain.BillingChargeBatch{}, mapStoreError(err)
	}
	if snapshot.Batch.Status != domain.BillingChargeStatusFailed {
		return domain.BillingChargeBatch{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.BillingChargeBatch{}, err
	}
	audit := auditContext(command, domain.AuditActionBillingChargeRetry, "billing_charge_batch", id, billingBatchState(snapshot.Batch), nil, at)
	batch, err := s.deps.BatchRetrier.RetryFailedBatch(ctx, id, audit)
	if err != nil {
		switch {
		case errors.Is(err, billingapp.ErrChargeBatchNotFound):
			return domain.BillingChargeBatch{}, ErrNotFound
		case errors.Is(err, billingapp.ErrChargeBatchNotFailed), errors.Is(err, billingapp.ErrChargeReconciliationRequired):
			return domain.BillingChargeBatch{}, ErrStateConflict
		case errors.Is(err, billingapp.ErrBillingStoreUnavailable), errors.Is(err, billingapp.ErrBillingUnavailable):
			return domain.BillingChargeBatch{}, ErrStoreUnavailable
		default:
			return domain.BillingChargeBatch{}, ErrInternal
		}
	}
	return batch, nil
}

func (s *Service) ListAuditEntries(ctx context.Context, input AuditListInput) (ListResult[domain.AdminAuditEntry], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[domain.AdminAuditEntry]{}, err
	}
	if err := validateWindow(input.CreatedFrom, input.CreatedTo); err != nil {
		return ListResult[domain.AdminAuditEntry]{}, err
	}
	page, err := s.deps.Audit.ListAuditEntries(ctx, ports.AuditListFilter{AdminSubject: input.AdminSubject, Action: input.Action, EntityType: input.EntityType, EntityID: input.EntityID, CreatedFrom: input.CreatedFrom, CreatedTo: input.CreatedTo, Page: pageReq})
	if err != nil {
		return ListResult[domain.AdminAuditEntry]{}, mapStoreError(err)
	}
	return listResult(page, pageReq), nil
}
