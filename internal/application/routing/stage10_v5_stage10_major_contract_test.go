package routing

import (
	"math"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestStage10V5SelectRejectsOverflowedResellerAvailableBalance(t *testing.T) {
	candidate := Candidate{
		Route: domain.Route{
			ID:                 "route_a",
			ResellerID:         "reseller_a",
			ProviderType:       domain.ProviderOpenAI,
			APIFamily:          domain.APIFamilyOpenAICompatible,
			EndpointKind:       domain.EndpointChat,
			ClientModel:        "model-a",
			ProviderModel:      "model-a",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
			Enabled:            true,
			Capabilities:       domain.CapabilitySet{Chat: true},
		},
		Reseller: domain.Reseller{
			ID:                  "reseller_a",
			ProviderType:        domain.ProviderOpenAI,
			Enabled:             true,
			BalanceCents:        math.MinInt64,
			ReservedCents:       1,
			MinimumBalanceCents: 0,
		},
		ForwardingAdapterAvailable:    true,
		SecretAvailable:               true,
		CostAvailable:                 true,
		EstimatedUpstreamCostCents:    1,
		RateLimitAllowed:              true,
		ConcurrencyAllowed:            true,
		ModelIdentifierRewriteAllowed: true,
	}
	result, err := Select(SelectionInput{
		Query: ports.RouteQuery{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  "model-a",
		},
		RequestedCapabilities: domain.CapabilitySet{Chat: true},
		Candidates:            []Candidate{candidate},
		Now:                   time.Unix(100, 0).UTC(),
	})
	if err != ErrNoRouteAvailable {
		t.Fatalf("error = %v", err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != SkipReasonInvalidResellerBalance {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
}
