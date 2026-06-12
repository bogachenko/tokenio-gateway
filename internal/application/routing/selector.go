package routing

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type SelectionInput struct {
	Query                 ports.RouteQuery
	RequestedCapabilities domain.CapabilitySet
	Candidates            []Candidate
	Now                   time.Time
}

type Candidate struct {
	Route    domain.Route
	Reseller domain.Reseller

	SecretAvailable bool

	CostAvailable              bool
	EstimatedUpstreamCostCents int64

	RateLimitAllowed              bool
	ConcurrencyAllowed            bool
	ModelIdentifierRewriteAllowed bool
}

type SelectionResult struct {
	Selected Candidate

	Fallbacks []Candidate
	Skipped   []SkippedRoute
}

func Select(input SelectionInput) (SelectionResult, error) {
	if err := validateInput(input); err != nil {
		return SelectionResult{}, err
	}

	structural := make([]Candidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		if matchesRouteKey(candidate.Route, input.Query) {
			structural = append(structural, candidate)
		}
	}
	if len(structural) == 0 {
		return SelectionResult{}, ErrUnknownModel
	}
	if err := validateStructuralCandidates(structural); err != nil {
		return SelectionResult{}, err
	}

	skipped := make([]SkippedRoute, 0, len(structural))
	compatible := make([]Candidate, 0, len(structural))
	for _, candidate := range structural {
		if !Supports(candidate.Route.Capabilities, input.RequestedCapabilities) {
			skipped = append(skipped, skip(candidate, SkipReasonMissingCapability))
			continue
		}
		compatible = append(compatible, candidate)
	}
	if len(compatible) == 0 {
		return SelectionResult{Skipped: skipped}, ErrUnsupportedCapability
	}

	eligible := make([]Candidate, 0, len(compatible))
	for _, candidate := range compatible {
		if reason, ok := primaryOperationalSkipReason(candidate, input.Now); ok {
			skipped = append(skipped, skip(candidate, reason))
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return SelectionResult{Skipped: skipped}, ErrNoRouteAvailable
	}

	sort.Slice(eligible, func(i, j int) bool {
		left := eligible[i]
		right := eligible[j]
		if left.EstimatedUpstreamCostCents != right.EstimatedUpstreamCostCents {
			return left.EstimatedUpstreamCostCents < right.EstimatedUpstreamCostCents
		}
		if left.Route.Priority != right.Route.Priority {
			return left.Route.Priority < right.Route.Priority
		}
		return left.Route.ID < right.Route.ID
	})

	fallbacks := make([]Candidate, len(eligible)-1)
	copy(fallbacks, eligible[1:])
	return SelectionResult{
		Selected:  eligible[0],
		Fallbacks: fallbacks,
		Skipped:   skipped,
	}, nil
}

func validateInput(input SelectionInput) error {
	if input.Query.APIFamily == "" || input.Query.EndpointKind == "" || strings.TrimSpace(input.Query.ClientModel) == "" || input.Now.IsZero() {
		return ErrInvalidSelectionInput
	}
	return nil
}

func matchesRouteKey(route domain.Route, query ports.RouteQuery) bool {
	return route.APIFamily == query.APIFamily &&
		route.EndpointKind == query.EndpointKind &&
		route.ClientModel == query.ClientModel
}

func validateStructuralCandidates(candidates []Candidate) error {
	seenRouteIDs := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Route.ID == "" || candidate.Route.ResellerID == "" || candidate.Reseller.ID == "" {
			return fmt.Errorf("%w", ErrInvalidRouteData)
		}
		if candidate.Route.ResellerID != candidate.Reseller.ID {
			return fmt.Errorf("%w", ErrInvalidRouteData)
		}
		if candidate.Route.ProviderType != candidate.Reseller.ProviderType {
			return fmt.Errorf("%w", ErrInvalidRouteData)
		}
		if _, exists := seenRouteIDs[candidate.Route.ID]; exists {
			return fmt.Errorf("%w", ErrInvalidRouteData)
		}
		seenRouteIDs[candidate.Route.ID] = struct{}{}
	}
	return nil
}

func primaryOperationalSkipReason(candidate Candidate, now time.Time) (SkipReason, bool) {
	if !candidate.Route.Enabled || !candidate.Reseller.Enabled {
		return SkipReasonManualDisabled, true
	}
	if !candidate.SecretAvailable {
		return SkipReasonMissingResellerAPIKey, true
	}
	if candidate.Route.CooldownUntil != nil && candidate.Route.CooldownUntil.After(now) {
		return SkipReasonCooldownActive, true
	}
	if !candidate.CostAvailable {
		return SkipReasonPricingUnavailable, true
	}
	if candidate.EstimatedUpstreamCostCents < 0 {
		return SkipReasonInvalidRoutePrice, true
	}
	if availableResellerBalanceCents(candidate.Reseller) < candidate.EstimatedUpstreamCostCents {
		return SkipReasonInsufficientResellerBalance, true
	}
	if !candidate.RateLimitAllowed {
		return SkipReasonRateLimitExceeded, true
	}
	if !candidate.ConcurrencyAllowed {
		return SkipReasonConcurrencyLimitExceeded, true
	}
	if !modelRewritePolicyAllowed(candidate) {
		return SkipReasonUnsupportedModelRewritePolicy, true
	}
	return "", false
}

func availableResellerBalanceCents(reseller domain.Reseller) int64 {
	return reseller.BalanceCents - reseller.ReservedCents - reseller.MinimumBalanceCents
}

func modelRewritePolicyAllowed(candidate Candidate) bool {
	switch candidate.Route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return candidate.Route.ProviderModel == candidate.Route.ClientModel
	case domain.ModelRewritePolicyProviderModel:
		return strings.TrimSpace(candidate.Route.ProviderModel) != "" && candidate.ModelIdentifierRewriteAllowed
	default:
		return false
	}
}

func skip(candidate Candidate, reason SkipReason) SkippedRoute {
	return SkippedRoute{RouteID: candidate.Route.ID, ResellerID: candidate.Reseller.ID, Reason: reason}
}
