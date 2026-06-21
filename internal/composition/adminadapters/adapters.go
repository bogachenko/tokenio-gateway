package adminadapters

import (
	"context"
	"errors"

	adminapp "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type FailedBatchRetrierSource interface {
	RetryFailedBatch(
		context.Context,
		string,
		domain.AuditContext,
	) (domain.BillingChargeBatch, error)
}

type failedBatchRetrierAdapter struct {
	next FailedBatchRetrierSource
}

func NewFailedBatchRetrierAdapter(
	next FailedBatchRetrierSource,
) adminapp.FailedChargeBatchRetrier {
	return &failedBatchRetrierAdapter{next: next}
}

func (a *failedBatchRetrierAdapter) RetryFailedBatch(
	ctx context.Context,
	batchID string,
	audit domain.AuditContext,
) (domain.BillingChargeBatch, error) {
	if a == nil || a.next == nil {
		return domain.BillingChargeBatch{}, adminapp.ErrBatchRetryInternal
	}

	batch, err := a.next.RetryFailedBatch(ctx, batchID, audit)
	if err == nil {
		return batch, nil
	}

	switch {
	case errors.Is(err, billingapp.ErrChargeBatchNotFound):
		return batch, adminapp.ErrBatchRetryNotFound
	case errors.Is(err, billingapp.ErrChargeBatchNotFailed), errors.Is(err, billingapp.ErrChargeReconciliationRequired):
		return batch, adminapp.ErrBatchRetryStateConflict
	case errors.Is(err, billingapp.ErrBillingStoreUnavailable), errors.Is(err, billingapp.ErrBillingUnavailable):
		return batch, adminapp.ErrBatchRetryUnavailable
	default:
		return batch, adminapp.ErrBatchRetryInternal
	}
}

type RoutePriceValidatorAdapter struct{}

func (RoutePriceValidatorAdapter) ValidateRoutePrice(price domain.RoutePrice) error {
	return pricingapp.ValidateRoutePrice(price)
}

type UsagePolicyAdapter struct{}

func (UsagePolicyAdapter) ValidateRecord(record domain.UsageRecord) error {
	return ledgerapp.ValidateRecord(record)
}

func (UsagePolicyAdapter) ValidateTransition(from domain.UsageStatus, to domain.UsageStatus) error {
	return ledgerapp.ValidateTransition(from, to)
}
