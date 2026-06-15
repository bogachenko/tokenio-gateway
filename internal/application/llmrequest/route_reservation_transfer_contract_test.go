package llmrequest

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestRouteReservationTransferInputCarriesExactSnapshots(
	t *testing.T,
) {
	expected := domain.UsageRecord{
		LocalRequestID:             "llmreq-1",
		SelectedRouteID:            "route-primary",
		SelectedResellerID:         "reseller-primary",
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              "provider-primary",
		EstimatedUpstreamCostCents: 17,
		EstimatedClientAmountCents: 23,
		Currency:                   "RUB",
		Status:                     domain.UsageStatusReserved,
	}
	target := RouteFallbackPlan{
		Route: domain.Route{
			ID:            "route-fallback",
			ResellerID:    "reseller-fallback",
			ProviderType:  domain.ProviderOpenRouter,
			ProviderModel: "provider-fallback",
		},
		Reseller: domain.Reseller{
			ID:           "reseller-fallback",
			ProviderType: domain.ProviderOpenRouter,
		},
		BillingModel:               "billing-model",
		EstimatedUsage:             domain.TokenUsage{InputTokens: 7},
		EstimatedClientAmountCents: 29,
		EstimatedUpstreamCostCents: 19,
		Currency:                   "RUB",
		Confidence:                 "exact",
	}

	input := RouteReservationTransferInput{
		ExpectedUsage: expected,
		Target:        target,
	}

	if input.ExpectedUsage.LocalRequestID != "llmreq-1" ||
		input.ExpectedUsage.SelectedRouteID != "route-primary" ||
		input.ExpectedUsage.SelectedResellerID != "reseller-primary" ||
		input.Target.Route.ID != "route-fallback" ||
		input.Target.Reseller.ID != "reseller-fallback" ||
		input.Target.EstimatedUpstreamCostCents != 19 {
		t.Fatalf("transfer input = %+v", input)
	}
}

func TestRouteReservationTransferResultCarriesCommittedState(
	t *testing.T,
) {
	result := RouteReservationTransferResult{
		Usage: domain.UsageRecord{
			LocalRequestID:     "llmreq-1",
			SelectedRouteID:    "route-fallback",
			SelectedResellerID: "reseller-fallback",
			Status:             domain.UsageStatusReserved,
		},
		ReleasedReseller: domain.Reseller{
			ID:            "reseller-primary",
			ReservedCents: 4,
		},
		ReservedReseller: domain.Reseller{
			ID:            "reseller-fallback",
			ReservedCents: 9,
		},
	}

	if result.Usage.SelectedRouteID != "route-fallback" ||
		result.ReleasedReseller.ID != "reseller-primary" ||
		result.ReservedReseller.ID != "reseller-fallback" {
		t.Fatalf("transfer result = %+v", result)
	}
}
