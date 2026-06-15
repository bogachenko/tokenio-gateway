package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestRouteTransferTargetUsageReplacesOnlyReservedSnapshot(
	t *testing.T,
) {
	expected := validRouteTransferUsage()
	target := validRouteTransferPlan()
	now := expected.UpdatedAt.Add(time.Second)

	actual := routeTransferTargetUsage(expected, target, now)

	if actual.LocalRequestID != expected.LocalRequestID ||
		actual.UserID != expected.UserID ||
		actual.Status != domain.UsageStatusReserved ||
		actual.SelectedRouteID != target.Route.ID ||
		actual.SelectedResellerID != target.Reseller.ID ||
		actual.ProviderType != target.Route.ProviderType ||
		actual.ProviderModel != target.Route.ProviderModel ||
		actual.BillingModel != target.BillingModel ||
		actual.EstimatedUsage != target.EstimatedUsage ||
		actual.EstimatedClientAmountCents !=
			target.EstimatedClientAmountCents ||
		actual.EstimatedUpstreamCostCents !=
			target.EstimatedUpstreamCostCents ||
		!actual.UpdatedAt.Equal(now) {
		t.Fatalf("target usage = %+v", actual)
	}
}

func TestRouteTransferAlreadyAppliedIgnoresOnlyUpdateTimestamp(
	t *testing.T,
) {
	expected := validRouteTransferUsage()
	target := routeTransferTargetUsage(
		expected,
		validRouteTransferPlan(),
		expected.UpdatedAt.Add(time.Second),
	)
	current := target
	current.UpdatedAt = target.UpdatedAt.Add(time.Second)

	if !routeTransferAlreadyApplied(current, target) {
		t.Fatal("identical committed transfer was not recognized")
	}
	current.ProviderModel = "different"
	if routeTransferAlreadyApplied(current, target) {
		t.Fatal("conflicting route snapshot was accepted")
	}
}

func TestValidateRouteReservationTransferInput(t *testing.T) {
	input := llmrequest.RouteReservationTransferInput{
		ExpectedUsage: validRouteTransferUsage(),
		Target:        validRouteTransferPlan(),
	}
	if err := validateRouteReservationTransferInput(input); err != nil {
		t.Fatalf("valid input: %v", err)
	}

	input.Target.Route.ResellerID = "other"
	if !errors.Is(
		validateRouteReservationTransferInput(input),
		ports.ErrStoreContractViolation,
	) {
		t.Fatal("route/reseller mismatch accepted")
	}
}

func validRouteTransferUsage() domain.UsageRecord {
	now := time.Date(
		2026,
		time.June,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	reservedAt := now
	return domain.UsageRecord{
		LocalRequestID:             "llmreq_test",
		UserID:                     "user-1",
		APIKeyID:                   "key-1",
		APIFamily:                  domain.APIFamilyOpenAICompatible,
		EndpointKind:               domain.EndpointChat,
		ClientModel:                "client-model",
		BillingModel:               "billing-primary",
		SelectedRouteID:            "route-primary",
		SelectedResellerID:         "reseller-primary",
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              "provider-primary",
		EstimatedUsage:             domain.TokenUsage{InputTokens: 7},
		EstimatedClientAmountCents: 23,
		EstimatedUpstreamCostCents: 17,
		Currency:                   "RUB",
		UsageCompleteness:          "missing",
		Status:                     domain.UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 &reservedAt,
		UpdatedAt:                  now,
	}
}

func validRouteTransferPlan() llmrequest.RouteFallbackPlan {
	return llmrequest.RouteFallbackPlan{
		Route: domain.Route{
			ID:            "route-fallback",
			ResellerID:    "reseller-fallback",
			ProviderType:  domain.ProviderOpenRouter,
			APIFamily:     domain.APIFamilyOpenAICompatible,
			EndpointKind:  domain.EndpointChat,
			ClientModel:   "client-model",
			ProviderModel: "provider-fallback",
			Enabled:       true,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-fallback",
			ProviderType: domain.ProviderOpenRouter,
			Enabled:      true,
		},
		BillingModel:               "billing-fallback",
		EstimatedUsage:             domain.TokenUsage{InputTokens: 9},
		EstimatedClientAmountCents: 29,
		EstimatedUpstreamCostCents: 19,
		Currency:                   "RUB",
		Confidence:                 "exact",
	}
}
