package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestFinancialContracts(t *testing.T) {
	model, err := BillingModel(ProviderType("provider-a"), "model-a")
	if err != nil || model != "provider-a:model-a" {
		t.Fatalf("BillingModel = %q, %v", model, err)
	}
	exposure, err := CalculateUsageExposure(CurrencyRUB, 10, 20, 5, 0)
	if err != nil {
		t.Fatalf("CalculateUsageExposure: %v", err)
	}
	result, err := EvaluateBalance(BalanceInput{
		RemoteBalanceCents:   100,
		RequiredReserveCents: 60,
		Exposure:             exposure,
	})
	if err != nil || !result.Allowed || result.EffectiveBalanceCents != 65 {
		t.Fatalf("EvaluateBalance = %+v, %v", result, err)
	}
	if _, err := CalculateUsageExposure(CurrencyRUB, math.MaxInt64, 1, 0, 0); !errors.Is(err, ErrFinancialAmountOverflow) {
		t.Fatalf("expected overflow, got %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := UsageRecord{
		LocalRequestID:             "llmreq_1",
		UserID:                     "user-1",
		APIKeyID:                   "key-1",
		APIFamily:                  APIFamilyOpenAICompatible,
		EndpointKind:               EndpointChat,
		ClientModel:                "model-a",
		BillingModel:               "provider-a:model-a",
		SelectedRouteID:            "route-1",
		SelectedResellerID:         "reseller-1",
		ProviderType:               ProviderType("provider-a"),
		ProviderModel:              "provider-model",
		EstimatedClientAmountCents: 10,
		EstimatedUpstreamCostCents: 5,
		Currency:                   CurrencyRUB,
		UsageCompleteness:          string(UsageCompletenessMissing),
		Status:                     UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 &now,
		UpdatedAt:                  now,
	}
	if err := ValidateUsageRecord(record); err != nil {
		t.Fatalf("ValidateUsageRecord: %v", err)
	}
}
