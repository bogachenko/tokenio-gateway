package app

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewRuntimePrimitivesWiresRouteCapacityManager(t *testing.T) {
	primitives, err := NewRuntimePrimitives()
	if err != nil {
		t.Fatalf("NewRuntimePrimitives: %v", err)
	}
	if primitives.RouteCapacity == nil {
		t.Fatal("route capacity manager is nil")
	}
	if err := primitives.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	result, err := primitives.RouteCapacity.Check(
		context.Background(),
		ports.RouteCapacityCheckInput{
			Route: domain.Route{
				ID:           "route-1",
				ResellerID:   "reseller-1",
				ProviderType: domain.ProviderOpenAI,
			},
			Reseller: domain.Reseller{
				ID:           "reseller-1",
				ProviderType: domain.ProviderOpenAI,
			},
			EstimatedUsage: domain.TokenUsage{
				InputTokens: 1,
			},
		},
	)
	if err != nil {
		t.Fatalf("route capacity Check: %v", err)
	}
	if !result.RateLimitAllowed ||
		!result.ConcurrencyAllowed {
		t.Fatalf("unexpected route capacity result: %+v", result)
	}
}

func TestRuntimePrimitivesValidateRequiresRouteCapacityManager(
	t *testing.T,
) {
	primitives, err := NewRuntimePrimitives()
	if err != nil {
		t.Fatalf("NewRuntimePrimitives: %v", err)
	}
	primitives.RouteCapacity = nil

	if err := primitives.Validate(); err == nil {
		t.Fatal("expected missing route capacity manager error")
	}
}
