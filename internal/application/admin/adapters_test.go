package admin

import (
	"context"
	"errors"
	"testing"

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

func TestFailedBatchRetrierAdapterMapsBoundaryErrors(
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
			want: ErrBatchRetryNotFound,
		},
		{
			name: "not failed",
			err:  billingapp.ErrChargeBatchNotFailed,
			want: ErrBatchRetryStateConflict,
		},
		{
			name: "reconciliation required",
			err: billingapp.
				ErrChargeReconciliationRequired,
			want: ErrBatchRetryStateConflict,
		},
		{
			name: "billing store unavailable",
			err: billingapp.
				ErrBillingStoreUnavailable,
			want: ErrBatchRetryUnavailable,
		},
		{
			name: "billing unavailable",
			err:  billingapp.ErrBillingUnavailable,
			want: ErrBatchRetryUnavailable,
		},
		{
			name: "unknown",
			err:  errors.New("unknown billing failure"),
			want: ErrBatchRetryInternal,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := NewFailedBatchRetrierAdapter(
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

func TestFailedBatchRetrierAdapterPreservesSuccess(
	t *testing.T,
) {
	want := domain.BillingChargeBatch{
		ID: "billchg_success",
	}
	adapter := NewFailedBatchRetrierAdapter(
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

func TestFailedBatchRetrierAdapterRejectsNilSource(
	t *testing.T,
) {
	adapter := NewFailedBatchRetrierAdapter(nil)
	_, err := adapter.RetryFailedBatch(
		context.Background(),
		"billchg_test",
		domain.AuditContext{},
	)
	if !errors.Is(err, ErrBatchRetryInternal) {
		t.Fatalf(
			"error=%v want=%v",
			err,
			ErrBatchRetryInternal,
		)
	}
}

func TestRoutePriceValidatorAdapterDelegatesPolicy(
	t *testing.T,
) {
	err := (RoutePriceValidatorAdapter{}).ValidateRoutePrice(
		domain.RoutePrice{},
	)
	if err == nil {
		t.Fatal("expected canonical pricing validation error")
	}
}

func TestUsagePolicyAdapterDelegatesRecordAndTransitionPolicy(
	t *testing.T,
) {
	policy := UsagePolicyAdapter{}

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
