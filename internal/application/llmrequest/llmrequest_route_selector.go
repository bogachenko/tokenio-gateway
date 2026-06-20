package llmrequest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestRouteSelector struct {
	clock ports.Clock
}

var _ RouteCandidateSelector = (*LLMRequestRouteSelector)(nil)

func NewLLMRequestRouteSelector(
	clock ports.Clock,
) (*LLMRequestRouteSelector, error) {
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: nil route selector clock",
			ErrDependencyRequired,
		)
	}
	return &LLMRequestRouteSelector{
		clock: clock,
	}, nil
}

func (s *LLMRequestRouteSelector) Select(
	ctx context.Context,
	input RouteSelectionInput,
) (RouteSelectionResult, error) {
	if s == nil || s.clock == nil {
		return RouteSelectionResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return RouteSelectionResult{}, fmt.Errorf(
			"%w: nil route selector context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return RouteSelectionResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() {
		return RouteSelectionResult{}, fmt.Errorf(
			"%w: zero route selector clock",
			ErrStageContractViolation,
		)
	}

	selected, err := selectLLMRequestRouteCandidate(input, now)
	if err != nil {
		return RouteSelectionResult{}, err
	}

	fallbackRouteIDs := make([]string, len(selected.Fallbacks))
	for index, fallback := range selected.Fallbacks {
		fallbackRouteIDs[index] = fallback.Route.ID
	}
	return RouteSelectionResult{
		SelectedRouteID:  selected.Selected.Route.ID,
		FallbackRouteIDs: fallbackRouteIDs,
	}, nil
}

type llmRequestSelectionResult struct {
	Selected  RouteSelectionCandidate
	Fallbacks []RouteSelectionCandidate
	Skipped   []llmRequestSkippedRoute
}

type llmRequestSkippedRoute struct {
	RouteID    string
	ResellerID string
	Reason     llmRequestSkipReason
}

type llmRequestSkipReason string

const (
	llmRequestSkipReasonMissingCapability             llmRequestSkipReason = "missing_capability"
	llmRequestSkipReasonManualDisabled                llmRequestSkipReason = "manual_disabled"
	llmRequestSkipReasonForwardingAdapterUnavailable  llmRequestSkipReason = "forwarding_adapter_unavailable"
	llmRequestSkipReasonMissingResellerAPIKey         llmRequestSkipReason = "missing_reseller_api_key"
	llmRequestSkipReasonCooldownActive                llmRequestSkipReason = "cooldown_active"
	llmRequestSkipReasonPricingUnavailable            llmRequestSkipReason = "pricing_unavailable"
	llmRequestSkipReasonInvalidRoutePrice             llmRequestSkipReason = "invalid_route_price"
	llmRequestSkipReasonInvalidResellerBalance        llmRequestSkipReason = "invalid_reseller_balance"
	llmRequestSkipReasonInsufficientResellerBalance   llmRequestSkipReason = "insufficient_reseller_balance"
	llmRequestSkipReasonRateLimitExceeded             llmRequestSkipReason = "rate_limit_exceeded"
	llmRequestSkipReasonConcurrencyLimitExceeded      llmRequestSkipReason = "concurrency_limit_exceeded"
	llmRequestSkipReasonUnsupportedModelRewritePolicy llmRequestSkipReason = "unsupported_model_rewrite_policy"
)

func selectLLMRequestRouteCandidate(
	input RouteSelectionInput,
	now time.Time,
) (llmRequestSelectionResult, error) {
	if input.APIFamily == "" || input.EndpointKind == "" || strings.TrimSpace(input.ClientModel) == "" || now.IsZero() {
		return llmRequestSelectionResult{}, fmt.Errorf(
			"%w: invalid route selection input",
			ErrStageContractViolation,
		)
	}

	structural := make([]RouteSelectionCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		if candidate.Route.APIFamily == input.APIFamily &&
			candidate.Route.EndpointKind == input.EndpointKind &&
			candidate.Route.ClientModel == input.ClientModel {
			structural = append(structural, candidate)
		}
	}
	if len(structural) == 0 {
		return llmRequestSelectionResult{}, ErrUnknownModel
	}
	if err := validateStructuralRouteSelectionCandidates(structural); err != nil {
		return llmRequestSelectionResult{}, err
	}

	skipped := make([]llmRequestSkippedRoute, 0, len(structural))
	compatible := make([]RouteSelectionCandidate, 0, len(structural))
	for _, candidate := range structural {
		if !supportsRouteCapabilities(candidate.Route.Capabilities, input.RequestedCapabilities) {
			skipped = append(skipped, skipLLMRequestRoute(candidate, llmRequestSkipReasonMissingCapability))
			continue
		}
		compatible = append(compatible, candidate)
	}
	if len(compatible) == 0 {
		return llmRequestSelectionResult{Skipped: skipped}, ErrUnsupportedCapability
	}

	eligible := make([]RouteSelectionCandidate, 0, len(compatible))
	for _, candidate := range compatible {
		if reason, ok := primaryLLMRequestOperationalSkipReason(candidate, now); ok {
			skipped = append(skipped, skipLLMRequestRoute(candidate, reason))
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		if compatibleRoutesOnlyLackPricing(skipped) {
			return llmRequestSelectionResult{Skipped: skipped}, ErrPricingUnavailable
		}
		return llmRequestSelectionResult{Skipped: skipped}, ErrNoRouteAvailable
	}

	sort.Slice(eligible, func(i, j int) bool {
		left := eligible[i]
		right := eligible[j]
		if left.Preflight.EstimatedUpstreamCostCents != right.Preflight.EstimatedUpstreamCostCents {
			return left.Preflight.EstimatedUpstreamCostCents < right.Preflight.EstimatedUpstreamCostCents
		}
		if left.Route.Priority != right.Route.Priority {
			return left.Route.Priority < right.Route.Priority
		}
		return left.Route.ID < right.Route.ID
	})

	fallbacks := make([]RouteSelectionCandidate, len(eligible)-1)
	copy(fallbacks, eligible[1:])
	return llmRequestSelectionResult{
		Selected:  eligible[0],
		Fallbacks: fallbacks,
		Skipped:   skipped,
	}, nil
}

func validateStructuralRouteSelectionCandidates(
	candidates []RouteSelectionCandidate,
) error {
	seenRouteIDs := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Route.ID == "" || candidate.Route.ResellerID == "" || candidate.Reseller.ID == "" {
			return fmt.Errorf("%w: invalid route data", ErrStageContractViolation)
		}
		if candidate.Route.ResellerID != candidate.Reseller.ID {
			return fmt.Errorf("%w: invalid route data", ErrStageContractViolation)
		}
		if candidate.Route.ProviderType != candidate.Reseller.ProviderType {
			return fmt.Errorf("%w: invalid route data", ErrStageContractViolation)
		}
		if _, exists := seenRouteIDs[candidate.Route.ID]; exists {
			return fmt.Errorf("%w: invalid route data", ErrStageContractViolation)
		}
		seenRouteIDs[candidate.Route.ID] = struct{}{}
	}
	return nil
}

func supportsRouteCapabilities(
	available domain.CapabilitySet,
	requested domain.CapabilitySet,
) bool {
	return (!requested.Chat || available.Chat) &&
		(!requested.Embeddings || available.Embeddings) &&
		(!requested.ImagesGeneration || available.ImagesGeneration) &&
		(!requested.Tools || available.Tools) &&
		(!requested.ToolChoice || available.ToolChoice) &&
		(!requested.ResponseFormat || available.ResponseFormat) &&
		(!requested.JSONSchema || available.JSONSchema) &&
		(!requested.ImageInput || available.ImageInput) &&
		(!requested.AudioInput || available.AudioInput) &&
		(!requested.FileInput || available.FileInput) &&
		(!requested.VideoInput || available.VideoInput) &&
		(!requested.Reasoning || available.Reasoning)
}

func primaryLLMRequestOperationalSkipReason(
	candidate RouteSelectionCandidate,
	now time.Time,
) (llmRequestSkipReason, bool) {
	if !candidate.Route.Enabled || !candidate.Reseller.Enabled {
		return llmRequestSkipReasonManualDisabled, true
	}
	if !candidate.Preflight.ForwardingAdapterAvailable {
		return llmRequestSkipReasonForwardingAdapterUnavailable, true
	}
	if !candidate.Preflight.SecretAvailable {
		return llmRequestSkipReasonMissingResellerAPIKey, true
	}
	if candidate.Route.CooldownUntil != nil && candidate.Route.CooldownUntil.After(now) {
		return llmRequestSkipReasonCooldownActive, true
	}
	if !candidate.Preflight.CostAvailable {
		return llmRequestSkipReasonPricingUnavailable, true
	}
	if candidate.Preflight.EstimatedUpstreamCostCents < 0 {
		return llmRequestSkipReasonInvalidRoutePrice, true
	}
	available, err := availableResellerBalanceCents(candidate.Reseller)
	if err != nil {
		return llmRequestSkipReasonInvalidResellerBalance, true
	}
	if available < candidate.Preflight.EstimatedUpstreamCostCents {
		return llmRequestSkipReasonInsufficientResellerBalance, true
	}
	if !candidate.Preflight.RateLimitAllowed {
		return llmRequestSkipReasonRateLimitExceeded, true
	}
	if !candidate.Preflight.ConcurrencyAllowed {
		return llmRequestSkipReasonConcurrencyLimitExceeded, true
	}
	if !modelRewritePolicyAllowed(candidate) {
		return llmRequestSkipReasonUnsupportedModelRewritePolicy, true
	}
	return "", false
}

func availableResellerBalanceCents(reseller domain.Reseller) (int64, error) {
	available, err := checkedSubInt64(reseller.BalanceCents, reseller.ReservedCents)
	if err != nil {
		return 0, err
	}
	return checkedSubInt64(available, reseller.MinimumBalanceCents)
}

func checkedSubInt64(left, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ErrStageContractViolation
	}
	if right < 0 && left > math.MaxInt64+right {
		return 0, ErrStageContractViolation
	}
	return left - right, nil
}

func modelRewritePolicyAllowed(candidate RouteSelectionCandidate) bool {
	switch candidate.Route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return candidate.Route.ProviderModel == candidate.Route.ClientModel
	case domain.ModelRewritePolicyProviderModel:
		return strings.TrimSpace(candidate.Route.ProviderModel) != "" && candidate.Preflight.ModelIdentifierRewriteAllowed
	default:
		return false
	}
}

func skipLLMRequestRoute(
	candidate RouteSelectionCandidate,
	reason llmRequestSkipReason,
) llmRequestSkippedRoute {
	return llmRequestSkippedRoute{
		RouteID:    candidate.Route.ID,
		ResellerID: candidate.Reseller.ID,
		Reason:     reason,
	}
}

func compatibleRoutesOnlyLackPricing(
	skipped []llmRequestSkippedRoute,
) bool {
	pricingUnavailable := 0
	for _, skippedRoute := range skipped {
		switch skippedRoute.Reason {
		case llmRequestSkipReasonMissingCapability:
			continue
		case llmRequestSkipReasonPricingUnavailable:
			pricingUnavailable++
		default:
			return false
		}
	}
	return pricingUnavailable > 0
}
