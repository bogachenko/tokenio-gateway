package llmrequest

import (
	"context"
	"errors"
	"testing"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type autoChargeServiceFunc func(
	context.Context,
	billingapp.AutoChargeInput,
) (billingapp.AutoChargeResult, error)

func (f autoChargeServiceFunc) Run(
	ctx context.Context,
	input billingapp.AutoChargeInput,
) (billingapp.AutoChargeResult, error) {
	return f(ctx, input)
}

func TestLLMRequestAutoChargerMapsProcessedResult(t *testing.T) {
	balance := int64(777)
	var got billingapp.AutoChargeInput
	adapter, err := NewLLMRequestAutoCharger(
		autoChargeServiceFunc(
			func(
				_ context.Context,
				input billingapp.AutoChargeInput,
			) (billingapp.AutoChargeResult, error) {
				got = input
				return billingapp.AutoChargeResult{
					ProcessedBatchIDs:   []string{"batch-1"},
					ChargedAmountCents:  15,
					BillingBalanceCents: &balance,
				}, nil
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	result := adapter.Run(
		context.Background(),
		AutoChargeInput{
			Principal: Principal{
				UserID:               "user-1",
				BillingSubjectUserID: "billing-1",
			},
			FinalUsageRecord: domain.UsageRecord{
				Currency: "RUB",
			},
		},
	)

	if got.UserID != "user-1" ||
		got.BillingSubjectUserID != "billing-1" ||
		got.Currency != "RUB" {
		t.Fatalf("input=%+v", got)
	}
	if result.Status != AutoChargeStatusProcessed ||
		result.ChargedAmountCents != 15 ||
		result.BillingBalanceCents == nil ||
		*result.BillingBalanceCents != 777 ||
		len(result.ProcessedBatchIDs) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestLLMRequestAutoChargerDoesNotEscalateBillingFailure(t *testing.T) {
	adapter, err := NewLLMRequestAutoCharger(
		autoChargeServiceFunc(
			func(context.Context, billingapp.AutoChargeInput) (billingapp.AutoChargeResult, error) {
				return billingapp.AutoChargeResult{}, errors.New("billing unavailable")
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	result := adapter.Run(context.Background(), AutoChargeInput{})
	if result.Status != AutoChargeStatusFailed {
		t.Fatalf("result=%+v", result)
	}
}

func TestLLMRequestAutoChargerMapsDeferred(t *testing.T) {
	adapter, err := NewLLMRequestAutoCharger(
		autoChargeServiceFunc(
			func(context.Context, billingapp.AutoChargeInput) (billingapp.AutoChargeResult, error) {
				return billingapp.AutoChargeResult{}, billingapp.ErrChargeDeferred
			},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	result := adapter.Run(context.Background(), AutoChargeInput{})
	if result.Status != AutoChargeStatusDeferred {
		t.Fatalf("result=%+v", result)
	}
}
