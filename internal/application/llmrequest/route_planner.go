package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type RouteCandidatePreflightInput struct {
	Route    domain.Route
	Reseller domain.Reseller
	Price    *domain.RoutePrice

	RequestedCapabilities domain.CapabilitySet
	Payload               []byte
}

type RouteCandidatePreflightResult struct {
	SecretAvailable bool
	CostAvailable   bool

	EstimatedUsage domain.TokenUsage

	EstimatedClientAmountCents int64
	EstimatedUpstreamCostCents int64

	Currency   string
	Confidence string

	ForwardingAdapterAvailable    bool
	RateLimitAllowed              bool
	ConcurrencyAllowed            bool
	ModelIdentifierRewriteAllowed bool
}

type RouteCandidatePreflighter interface {
	Evaluate(
		context.Context,
		RouteCandidatePreflightInput,
	) (RouteCandidatePreflightResult, error)
}

type RouteSelectionCandidate struct {
	Route    domain.Route
	Reseller domain.Reseller
	Price    *domain.RoutePrice

	Preflight RouteCandidatePreflightResult
}

type RouteSelectionInput struct {
	APIFamily             domain.APIFamily
	EndpointKind          domain.EndpointKind
	ClientModel           string
	RequestedCapabilities domain.CapabilitySet
	Candidates            []RouteSelectionCandidate
}

type RouteSelectionResult struct {
	SelectedRouteID  string
	FallbackRouteIDs []string
}

type RouteCandidateSelector interface {
	Select(
		context.Context,
		RouteSelectionInput,
	) (RouteSelectionResult, error)
}

type RepositoryRoutePlanner struct {
	routes      ports.RouteRepository
	resellers   ports.ResellerQueryRepository
	prices      ports.RoutePriceRepository
	preflighter RouteCandidatePreflighter
	selector    RouteCandidateSelector
	operational ports.RouteCooldownStore
	clock       ports.Clock
}

var _ RoutePlanner = (*RepositoryRoutePlanner)(nil)

func NewRepositoryRoutePlanner(
	routes ports.RouteRepository,
	resellers ports.ResellerQueryRepository,
	prices ports.RoutePriceRepository,
	preflighter RouteCandidatePreflighter,
	selector RouteCandidateSelector,
	operational ports.RouteCooldownStore,
	clock ports.Clock,
) (*RepositoryRoutePlanner, error) {
	if routes == nil ||
		resellers == nil ||
		prices == nil ||
		preflighter == nil ||
		selector == nil ||
		operational == nil ||
		clock == nil {
		return nil, ErrDependencyRequired
	}
	return &RepositoryRoutePlanner{
		routes:      routes,
		resellers:   resellers,
		prices:      prices,
		preflighter: preflighter,
		selector:    selector,
		operational: operational,
		clock:       clock,
	}, nil
}

func (p *RepositoryRoutePlanner) Plan(
	ctx context.Context,
	input RoutePlanInput,
) (RoutePlan, error) {
	if p == nil ||
		p.routes == nil ||
		p.resellers == nil ||
		p.prices == nil ||
		p.preflighter == nil ||
		p.selector == nil ||
		p.operational == nil ||
		p.clock == nil {
		return RoutePlan{}, ErrDependencyRequired
	}
	if ctx == nil {
		return RoutePlan{}, fmt.Errorf(
			"%w: nil route planner context",
			ErrInvalidInput,
		)
	}
	if err := validateRoutePlanInput(input); err != nil {
		return RoutePlan{}, err
	}

	routes, err := p.routes.FindRoutes(
		ctx,
		ports.RouteQuery{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			ClientModel:  input.ClientModel,
		},
	)
	if err != nil {
		return RoutePlan{}, fmt.Errorf(
			"find route candidates: %w",
			err,
		)
	}
	if len(routes) == 0 {
		return RoutePlan{}, ErrUnknownModel
	}

	canonicalRoutes, resellerIDs, routeIDs, err :=
		canonicalRouteCandidates(input, routes)
	if err != nil {
		return RoutePlan{}, err
	}
	canonicalRoutes, err = p.expireCooldowns(ctx, input, canonicalRoutes)
	if err != nil {
		return RoutePlan{}, err
	}

	resellers, err := p.resellers.FindByIDs(ctx, resellerIDs)
	if err != nil {
		return RoutePlan{}, fmt.Errorf(
			"find candidate resellers: %w",
			err,
		)
	}
	prices, err := p.prices.FindByRouteIDs(ctx, routeIDs)
	if err != nil {
		return RoutePlan{}, fmt.Errorf(
			"find candidate route prices: %w",
			err,
		)
	}

	candidates := make(
		[]RouteSelectionCandidate,
		0,
		len(canonicalRoutes),
	)
	for _, route := range canonicalRoutes {
		reseller, exists := resellers[route.ResellerID]
		if !exists {
			return RoutePlan{}, fmt.Errorf(
				"%w: reseller %q missing for route %q",
				ErrStageContractViolation,
				route.ResellerID,
				route.ID,
			)
		}
		if reseller.ID != route.ResellerID ||
			reseller.ProviderType != route.ProviderType {
			return RoutePlan{}, fmt.Errorf(
				"%w: reseller identity mismatch for route %q",
				ErrStageContractViolation,
				route.ID,
			)
		}

		var price *domain.RoutePrice
		if value, exists := prices[route.ID]; exists {
			priceCopy := value
			price = &priceCopy
		}

		preflight, err := p.preflighter.Evaluate(
			ctx,
			RouteCandidatePreflightInput{
				Route:                 route,
				Reseller:              reseller,
				Price:                 cloneRoutePricePointer(price),
				RequestedCapabilities: input.RequestedCapabilities,
				Payload:               cloneBytes(input.Payload),
			},
		)
		if err != nil {
			return RoutePlan{}, fmt.Errorf(
				"evaluate route %q preflight: %w",
				route.ID,
				err,
			)
		}

		candidates = append(
			candidates,
			RouteSelectionCandidate{
				Route:     route,
				Reseller:  reseller,
				Price:     cloneRoutePricePointer(price),
				Preflight: preflight,
			},
		)
	}

	selection, err := p.selector.Select(
		ctx,
		RouteSelectionInput{
			APIFamily:             input.APIFamily,
			EndpointKind:          input.EndpointKind,
			ClientModel:           input.ClientModel,
			RequestedCapabilities: input.RequestedCapabilities,
			Candidates:            cloneRouteSelectionCandidates(candidates),
		},
	)
	if err != nil {
		selectionErr := fmt.Errorf(
			"select route candidate: %w",
			err,
		)
		eventErr := p.recordSelectionEvents(
			ctx,
			input,
			candidates,
			RouteSelectionResult{},
		)
		return RoutePlan{}, errors.Join(selectionErr, eventErr)
	}
	if err := p.recordSelectionEvents(ctx, input, candidates, selection); err != nil {
		return RoutePlan{}, err
	}

	selected, err := selectedRouteCandidate(
		candidates,
		selection.SelectedRouteID,
	)
	if err != nil {
		return RoutePlan{}, err
	}
	if err := validateSelectedRouteCandidate(selected); err != nil {
		return RoutePlan{}, err
	}
	fallbackCandidates, err := selectedRouteFallbacks(
		candidates,
		selection.SelectedRouteID,
		selection.FallbackRouteIDs,
	)
	if err != nil {
		return RoutePlan{}, err
	}
	fallbacks := make(
		[]RouteFallbackPlan,
		len(fallbackCandidates),
	)
	for index, candidate := range fallbackCandidates {
		if err := validateSelectedRouteCandidate(candidate); err != nil {
			return RoutePlan{}, err
		}
		fallbacks[index] = routeFallbackPlan(candidate)
	}

	return RoutePlan{
		Route:          selected.Route,
		Reseller:       selected.Reseller,
		Price:          *selected.Price,
		BillingModel:   billingModel(selected.Route),
		EstimatedUsage: selected.Preflight.EstimatedUsage,
		EstimatedClientAmountCents: selected.Preflight.
			EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: selected.Preflight.
			EstimatedUpstreamCostCents,
		Currency:   selected.Preflight.Currency,
		Confidence: selected.Preflight.Confidence,
		Fallbacks:  fallbacks,
	}, nil
}

func validateRoutePlanInput(input RoutePlanInput) error {
	if !validLocalRequestID(input.LocalRequestID) ||
		strings.TrimSpace(input.Principal.UserID) == "" ||
		strings.TrimSpace(input.Principal.APIKeyID) == "" ||
		strings.TrimSpace(
			input.Principal.BillingSubjectUserID,
		) == "" ||
		input.APIFamily == "" ||
		input.EndpointKind == "" ||
		strings.TrimSpace(input.ClientModel) == "" ||
		input.Payload == nil {
		return ErrInvalidInput
	}
	return nil
}

func canonicalRouteCandidates(
	input RoutePlanInput,
	routes []domain.Route,
) ([]domain.Route, []string, []string, error) {
	canonical := append([]domain.Route(nil), routes...)
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].ID < canonical[right].ID
	})

	resellerSet := make(map[string]struct{}, len(canonical))
	routeSet := make(map[string]struct{}, len(canonical))
	resellerIDs := make([]string, 0, len(canonical))
	routeIDs := make([]string, 0, len(canonical))

	for _, route := range canonical {
		if strings.TrimSpace(route.ID) == "" ||
			strings.TrimSpace(route.ResellerID) == "" ||
			route.APIFamily != input.APIFamily ||
			route.EndpointKind != input.EndpointKind ||
			route.ClientModel != input.ClientModel {
			return nil, nil, nil, fmt.Errorf(
				"%w: invalid route repository result",
				ErrStageContractViolation,
			)
		}
		if _, exists := routeSet[route.ID]; exists {
			return nil, nil, nil, fmt.Errorf(
				"%w: duplicate route id %q",
				ErrStageContractViolation,
				route.ID,
			)
		}
		routeSet[route.ID] = struct{}{}
		routeIDs = append(routeIDs, route.ID)

		if _, exists := resellerSet[route.ResellerID]; !exists {
			resellerSet[route.ResellerID] = struct{}{}
			resellerIDs = append(resellerIDs, route.ResellerID)
		}
	}
	sort.Strings(resellerIDs)
	return canonical, resellerIDs, routeIDs, nil
}

func selectedRouteCandidate(
	candidates []RouteSelectionCandidate,
	selectedRouteID string,
) (RouteSelectionCandidate, error) {
	if strings.TrimSpace(selectedRouteID) == "" {
		return RouteSelectionCandidate{}, fmt.Errorf(
			"%w: selector returned blank route id",
			ErrStageContractViolation,
		)
	}

	var selected *RouteSelectionCandidate
	for index := range candidates {
		if candidates[index].Route.ID != selectedRouteID {
			continue
		}
		if selected != nil {
			return RouteSelectionCandidate{}, fmt.Errorf(
				"%w: selected route id %q is ambiguous",
				ErrStageContractViolation,
				selectedRouteID,
			)
		}
		value := candidates[index]
		selected = &value
	}
	if selected == nil {
		return RouteSelectionCandidate{}, fmt.Errorf(
			"%w: selector returned unknown route id %q",
			ErrStageContractViolation,
			selectedRouteID,
		)
	}
	return *selected, nil
}

func selectedRouteFallbacks(
	candidates []RouteSelectionCandidate,
	selectedRouteID string,
	fallbackRouteIDs []string,
) ([]RouteSelectionCandidate, error) {
	result := make(
		[]RouteSelectionCandidate,
		0,
		len(fallbackRouteIDs),
	)
	seen := map[string]struct{}{selectedRouteID: {}}
	byID := make(
		map[string]RouteSelectionCandidate,
		len(candidates),
	)
	for _, candidate := range candidates {
		byID[candidate.Route.ID] = candidate
	}
	for _, routeID := range fallbackRouteIDs {
		if strings.TrimSpace(routeID) == "" {
			return nil, fmt.Errorf(
				"%w: selector returned blank fallback route id",
				ErrStageContractViolation,
			)
		}
		if _, exists := seen[routeID]; exists {
			return nil, fmt.Errorf(
				"%w: selector returned duplicate fallback route id %q",
				ErrStageContractViolation,
				routeID,
			)
		}
		candidate, exists := byID[routeID]
		if !exists {
			return nil, fmt.Errorf(
				"%w: selector returned unknown fallback route id %q",
				ErrStageContractViolation,
				routeID,
			)
		}
		seen[routeID] = struct{}{}
		result = append(result, candidate)
	}
	return result, nil
}

func routeFallbackPlan(
	candidate RouteSelectionCandidate,
) RouteFallbackPlan {
	return RouteFallbackPlan{
		Route:          candidate.Route,
		Reseller:       candidate.Reseller,
		Price:          *candidate.Price,
		BillingModel:   billingModel(candidate.Route),
		EstimatedUsage: candidate.Preflight.EstimatedUsage,
		EstimatedClientAmountCents: candidate.Preflight.
			EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: candidate.Preflight.
			EstimatedUpstreamCostCents,
		Currency:   candidate.Preflight.Currency,
		Confidence: candidate.Preflight.Confidence,
	}
}

func validateSelectedRouteCandidate(
	selected RouteSelectionCandidate,
) error {
	preflight := selected.Preflight
	if selected.Price == nil ||
		selected.Price.RouteID != selected.Route.ID ||
		!preflight.SecretAvailable ||
		!preflight.CostAvailable ||
		!preflight.ForwardingAdapterAvailable ||
		!preflight.RateLimitAllowed ||
		!preflight.ConcurrencyAllowed ||
		!preflight.ModelIdentifierRewriteAllowed ||
		preflight.EstimatedClientAmountCents < 0 ||
		preflight.EstimatedUpstreamCostCents < 0 ||
		preflight.Currency != "RUB" ||
		strings.TrimSpace(preflight.Confidence) == "" ||
		!nonNegativeUsage(preflight.EstimatedUsage) {
		return fmt.Errorf(
			"%w: selector returned ineligible route %q",
			ErrStageContractViolation,
			selected.Route.ID,
		)
	}
	return nil
}

func billingModel(route domain.Route) string {
	return string(route.ProviderType) + ":" + route.ProviderModel
}

func cloneRoutePricePointer(
	value *domain.RoutePrice,
) *domain.RoutePrice {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneRouteSelectionCandidates(
	values []RouteSelectionCandidate,
) []RouteSelectionCandidate {
	result := make([]RouteSelectionCandidate, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Price = cloneRoutePricePointer(value.Price)
	}
	return result
}
