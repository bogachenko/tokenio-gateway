package ledger

import (
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestStage10V5PricingFailedManualResolutionTransitions(t *testing.T) {
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

func TestStage10V5ValidatePricingFailedRecord(t *testing.T) {
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

func TestStage10V5LedgerRejectsNonCanonicalBillingModelBeforePersistence(t *testing.T) {
	input := ReserveInput{
		LocalRequestID:             "llmreq_model_contract",
		UserID:                     "usr_1",
		APIKeyID:                   "ak_1",
		APIFamily:                  domain.APIFamilyOpenAICompatible,
		EndpointKind:               domain.EndpointChat,
		ClientModel:                "model-a",
		BillingModel:               "wrong:model",
		SelectedRouteID:            "route_1",
		SelectedResellerID:         "reseller_1",
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              "model-a",
		Currency:                   "RUB",
		EstimatedClientAmountCents: 1,
		EstimatedUpstreamCostCents: 1,
	}
	if err := validateReserveInput(input); err == nil {
		t.Fatal("non-canonical billing model was accepted by reserve validation")
	}

	at := time.Unix(20, 0).UTC()
	reservedAt := at
	record := domain.UsageRecord{
		LocalRequestID:             input.LocalRequestID,
		UserID:                     input.UserID,
		APIKeyID:                   input.APIKeyID,
		APIFamily:                  input.APIFamily,
		EndpointKind:               input.EndpointKind,
		ClientModel:                input.ClientModel,
		BillingModel:               input.BillingModel,
		SelectedRouteID:            input.SelectedRouteID,
		SelectedResellerID:         input.SelectedResellerID,
		ProviderType:               input.ProviderType,
		ProviderModel:              input.ProviderModel,
		Currency:                   input.Currency,
		UsageCompleteness:          "missing",
		Status:                     domain.UsageStatusReserved,
		EstimatedClientAmountCents: input.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: input.EstimatedUpstreamCostCents,
		CreatedAt:                  at,
		ReservedAt:                 &reservedAt,
		UpdatedAt:                  at,
	}
	if err := ValidateRecord(record); err == nil {
		t.Fatal("persisted record validation accepted non-canonical billing model")
	}

	input.BillingModel = "openai:model-a"
	if err := validateReserveInput(input); err != nil {
		t.Fatalf("canonical billing model rejected: %v", err)
	}
	record.BillingModel = input.BillingModel
	if err := ValidateRecord(record); err != nil {
		t.Fatalf("canonical ledger record rejected: %v", err)
	}
}
