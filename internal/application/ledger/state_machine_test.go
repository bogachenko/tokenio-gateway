package ledger

import (
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestPricingFailedManualResolutionTransitions(t *testing.T) {
	for _, target := range []domain.UsageStatus{
		domain.UsageStatusBillable,
		domain.UsageStatusFailed,
		domain.UsageStatusCharged,
	} {
		if err := ValidateTransition(domain.UsageStatusPricingFailed, target); err != nil {
			t.Fatalf("pricing_failed -> %s: %v", target, err)
		}
	}
	for _, target := range []domain.UsageStatus{
		domain.UsageStatusReserved,
		domain.UsageStatusReleased,
		domain.UsageStatusPartiallyCharged,
	} {
		if err := ValidateTransition(domain.UsageStatusPricingFailed, target); err == nil {
			t.Fatalf("pricing_failed -> %s unexpectedly allowed", target)
		}
	}
}

func TestValidatePricingFailedRecord(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	record := domain.UsageRecord{
		LocalRequestID:    "llmreq_pricing_failed",
		ProviderType:      domain.ProviderOpenAI,
		ClientModel:       "model-a",
		BillingModel:      "openai:model-a",
		Status:            domain.UsageStatusPricingFailed,
		UsageCompleteness: "failed",
		FailureReason:     "usage_extraction_failed",
		CreatedAt:         at,
		UpdatedAt:         at,
	}
	if err := ValidateRecord(record); err != nil {
		t.Fatal(err)
	}
	record.RemainingAmountCents = 1
	if err := ValidateRecord(record); err == nil {
		t.Fatal("pricing_failed record with pending amount was accepted")
	}
}
