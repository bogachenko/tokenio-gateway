package app

import (
	"context"
	"errors"
	"testing"

	adminapp "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type failedBatchRetrierSourceFake struct {
	batch domain.BillingChargeBatch
	err   error
}

func (f failedBatchRetrierSourceFake) RetryFailedBatch(
	context.Context,
	string,
	domain.AuditContext,
) (domain.BillingChargeBatch, error) {
	return f.batch, f.err
}

func TestAdminFailedBatchRetrierMapsBoundaryErrors(
	t *testing.T,
) {
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "not found",
			err:  billingapp.ErrChargeBatchNotFound,
			want: adminapp.ErrBatchRetryNotFound,
		},
		{
			name: "not failed",
			err:  billingapp.ErrChargeBatchNotFailed,
			want: adminapp.ErrBatchRetryStateConflict,
		},
		{
			name: "reconciliation required",
			err: billingapp.
				ErrChargeReconciliationRequired,
			want: adminapp.ErrBatchRetryStateConflict,
		},
		{
			name: "billing store unavailable",
			err: billingapp.
				ErrBillingStoreUnavailable,
			want: adminapp.ErrBatchRetryUnavailable,
		},
		{
			name: "billing unavailable",
			err:  billingapp.ErrBillingUnavailable,
			want: adminapp.ErrBatchRetryUnavailable,
		},
		{
			name: "unknown",
			err:  errors.New("unknown billing failure"),
			want: adminapp.ErrBatchRetryInternal,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := newAdminFailedBatchRetrier(
				failedBatchRetrierSourceFake{
					err: testCase.err,
				},
			)
			_, err := adapter.RetryFailedBatch(
				context.Background(),
				"billchg_test",
				domain.AuditContext{},
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf(
					"error=%v want=%v",
					err,
					testCase.want,
				)
			}
		})
	}
}

func TestAdminFailedBatchRetrierPreservesSuccess(
	t *testing.T,
) {
	want := domain.BillingChargeBatch{
		ID: "billchg_success",
	}
	adapter := newAdminFailedBatchRetrier(
		failedBatchRetrierSourceFake{
			batch: want,
		},
	)

	got, err := adapter.RetryFailedBatch(
		context.Background(),
		want.ID,
		domain.AuditContext{},
	)
	if err != nil {
		t.Fatalf("RetryFailedBatch: %v", err)
	}
	if got != want {
		t.Fatalf("batch=%+v want=%+v", got, want)
	}
}

func TestAdminFailedBatchRetrierRejectsNilSource(
	t *testing.T,
) {
	adapter := newAdminFailedBatchRetrier(nil)
	_, err := adapter.RetryFailedBatch(
		context.Background(),
		"billchg_test",
		domain.AuditContext{},
	)
	if !errors.Is(err, adminapp.ErrBatchRetryInternal) {
		t.Fatalf(
			"error=%v want=%v",
			err,
			adminapp.ErrBatchRetryInternal,
		)
	}
}

func TestAdminRoutePriceValidatorDelegatesPolicy(
	t *testing.T,
) {
	err := (adminRoutePriceValidator{}).ValidateRoutePrice(
		domain.RoutePrice{},
	)
	if err == nil {
		t.Fatal("expected canonical pricing validation error")
	}
}

func TestAdminUsagePolicyDelegatesRecordAndTransitionPolicy(
	t *testing.T,
) {
	policy := adminUsagePolicy{}

	if err := policy.ValidateRecord(domain.UsageRecord{}); err == nil {
		t.Fatal("expected canonical ledger record validation error")
	}
	if err := policy.ValidateTransition(
		domain.UsageStatus("unknown"),
		domain.UsageStatusCharged,
	); err == nil {
		t.Fatal("expected canonical ledger transition validation error")
	}
}
