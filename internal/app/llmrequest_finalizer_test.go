package app

import (
	"context"
	"testing"
	"time"

	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type finalizerLedger struct {
	commitInput  ledgerapp.CommitBillableInput
	failureInput ledgerapp.MarkPricingFailedInput
}

func (l *finalizerLedger) CommitBillable(
	_ context.Context,
	input ledgerapp.CommitBillableInput,
) (domain.UsageRecord, error) {
	l.commitInput = input
	return domain.UsageRecord{
		LocalRequestID: input.LocalRequestID,
		Status:         domain.UsageStatusBillable,
	}, nil
}

func (l *finalizerLedger) MarkPricingFailed(
	_ context.Context,
	input ledgerapp.MarkPricingFailedInput,
) (domain.UsageRecord, error) {
	l.failureInput = input
	return domain.UsageRecord{
		LocalRequestID: input.LocalRequestID,
		Status:         domain.UsageStatusPricingFailed,
	}, nil
}

func TestLLMRequestFinalizerCommitsResolvedUsage(t *testing.T) {
	ledger := &finalizerLedger{}
	finalizer, err := NewLLMRequestFinalizer(ledger)
	if err != nil {
		t.Fatal(err)
	}
	result, err := finalizer.Commit(
		context.Background(),
		llmrequest.FinalizationInput{
			Reserved: finalizerReservedRequest(),
			ResolvedUsage: llmrequest.UsageResolutionResult{
				Usage: domain.TokenUsage{
					InputTokens:  10,
					OutputTokens: 5,
				},
				Completeness:          "detailed",
				UpstreamCostCents:     7,
				ClientAmountCents:     11,
				Currency:              "RUB",
				ProviderRequestID:     "provider-request-1",
				ProviderResponseModel: "provider-model",
			},
		},
	)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Usage.Status != domain.UsageStatusBillable ||
		ledger.commitInput.LocalRequestID != "llmreq_finalizer_1" ||
		ledger.commitInput.ClientAmountCents != 11 ||
		ledger.commitInput.ActualUpstreamCostCents != 7 ||
		ledger.commitInput.ProviderRequestID != "provider-request-1" {
		t.Fatalf(
			"result=%+v input=%+v",
			result,
			ledger.commitInput,
		)
	}
}

func TestLLMRequestFinalizerMarksPricingFailure(t *testing.T) {
	ledger := &finalizerLedger{}
	finalizer, err := NewLLMRequestFinalizer(ledger)
	if err != nil {
		t.Fatal(err)
	}
	result, err := finalizer.MarkPricingFailed(
		context.Background(),
		llmrequest.PricingFailureInput{
			Reserved:      finalizerReservedRequest(),
			FailureReason: "usage_resolution_failed",
		},
	)
	if err != nil {
		t.Fatalf("MarkPricingFailed: %v", err)
	}
	if result.Usage.Status != domain.UsageStatusPricingFailed ||
		ledger.failureInput.LocalRequestID != "llmreq_finalizer_1" ||
		ledger.failureInput.FailureReason !=
			"usage_resolution_failed" ||
		ledger.failureInput.Usage.InputTokens != 20 {
		t.Fatalf(
			"result=%+v input=%+v",
			result,
			ledger.failureInput,
		)
	}
}

func finalizerReservedRequest() llmrequest.ReservedRequest {
	now := time.Date(
		2026,
		time.June,
		14,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	return llmrequest.ReservedRequest{
		Prepared: llmrequest.PreparedRequest{
			LocalRequestID: "llmreq_finalizer_1",
			Plan: llmrequest.RoutePlan{
				EstimatedUsage: domain.TokenUsage{
					InputTokens:  20,
					OutputTokens: 10,
				},
			},
		},
		Reservation: llmrequest.ReservationResult{
			Usage: domain.UsageRecord{
				LocalRequestID: "llmreq_finalizer_1",
				Status:         domain.UsageStatusReserved,
				ReservedAt:     &now,
			},
		},
	}
}
