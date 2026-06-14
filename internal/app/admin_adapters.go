package app

import (
	"context"
	"errors"

	adminapp "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type failedBatchRetrierSource interface {
	RetryFailedBatch(
		context.Context,
		string,
		domain.AuditContext,
	) (domain.BillingChargeBatch, error)
}

type adminFailedBatchRetrier struct {
	next failedBatchRetrierSource
}

func newAdminFailedBatchRetrier(
	next failedBatchRetrierSource,
) adminapp.FailedChargeBatchRetrier {
	return &adminFailedBatchRetrier{next: next}
}

func (a *adminFailedBatchRetrier) RetryFailedBatch(
	ctx context.Context,
	batchID string,
	audit domain.AuditContext,
) (domain.BillingChargeBatch, error) {
	if a == nil || a.next == nil {
		return domain.BillingChargeBatch{},
			adminapp.ErrBatchRetryInternal
	}

	batch, err := a.next.RetryFailedBatch(
		ctx,
		batchID,
		audit,
	)
	if err == nil {
		return batch, nil
	}

	switch {
	case errors.Is(err, billingapp.ErrChargeBatchNotFound):
		return batch, adminapp.ErrBatchRetryNotFound
	case errors.Is(err, billingapp.ErrChargeBatchNotFailed),
		errors.Is(
			err,
			billingapp.ErrChargeReconciliationRequired,
		):
		return batch, adminapp.ErrBatchRetryStateConflict
	case errors.Is(err, billingapp.ErrBillingStoreUnavailable),
		errors.Is(err, billingapp.ErrBillingUnavailable):
		return batch, adminapp.ErrBatchRetryUnavailable
	default:
		return batch, adminapp.ErrBatchRetryInternal
	}
}

type adminRoutePriceValidator struct{}

func (adminRoutePriceValidator) ValidateRoutePrice(
	price domain.RoutePrice,
) error {
	return pricingapp.ValidateRoutePrice(price)
}

type adminUsagePolicy struct{}

func (adminUsagePolicy) ValidateRecord(
	record domain.UsageRecord,
) error {
	return ledgerapp.ValidateRecord(record)
}

func (adminUsagePolicy) ValidateTransition(
	from domain.UsageStatus,
	to domain.UsageStatus,
) error {
	return ledgerapp.ValidateTransition(from, to)
}
