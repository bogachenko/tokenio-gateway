package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type routeRepositoryFunc func(
	context.Context,
	ports.RouteQuery,
) ([]domain.Route, error)

func (function routeRepositoryFunc) FindRoutes(
	ctx context.Context,
	query ports.RouteQuery,
) ([]domain.Route, error) {
	return function(ctx, query)
}

type resellerRepositoryFunc func(
	context.Context,
	[]string,
) (map[string]domain.Reseller, error)

func (function resellerRepositoryFunc) FindByIDs(
	ctx context.Context,
	ids []string,
) (map[string]domain.Reseller, error) {
	return function(ctx, ids)
}

type routePriceRepositoryFunc func(
	context.Context,
	[]string,
) (map[string]domain.RoutePrice, error)

func (function routePriceRepositoryFunc) FindByRouteIDs(
	ctx context.Context,
	ids []string,
) (map[string]domain.RoutePrice, error) {
	return function(ctx, ids)
}

type routeCandidatePreflightFunc func(
	context.Context,
	RouteCandidatePreflightInput,
) (RouteCandidatePreflightResult, error)

func (function routeCandidatePreflightFunc) Evaluate(
	ctx context.Context,
	input RouteCandidatePreflightInput,
) (RouteCandidatePreflightResult, error) {
	return function(ctx, input)
}

type routeCandidateSelectorFunc func(
	context.Context,
	RouteSelectionInput,
) (RouteSelectionResult, error)

func (function routeCandidateSelectorFunc) Select(
	ctx context.Context,
	input RouteSelectionInput,
) (RouteSelectionResult, error) {
	return function(ctx, input)
}

func TestRepositoryRoutePlannerBuildsDeterministicCandidateSet(
	t *testing.T,
) {
	input := validRoutePlanInput()
	routes := []domain.Route{
		validPlannerRoute("route-b", "reseller-b"),
		validPlannerRoute("route-a", "reseller-a"),
	}
	resellers := map[string]domain.Reseller{
		"reseller-a": validPlannerReseller("reseller-a"),
		"reseller-b": validPlannerReseller("reseller-b"),
	}
	prices := map[string]domain.RoutePrice{
		"route-a": validPlannerPrice("route-a"),
		"route-b": validPlannerPrice("route-b"),
	}

	var gotResellerIDs []string
	var gotRouteIDs []string
	var preflightOrder []string
	var selectorInput RouteSelectionInput

	planner := mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				_ context.Context,
				query ports.RouteQuery,
			) ([]domain.Route, error) {
				if query.APIFamily != input.APIFamily ||
					query.EndpointKind != input.EndpointKind ||
					query.ClientModel != input.ClientModel {
					t.Fatalf("query = %+v", query)
				}
				return routes, nil
			},
		),
		resellerRepositoryFunc(
			func(
				_ context.Context,
				ids []string,
			) (map[string]domain.Reseller, error) {
				gotResellerIDs = append([]string(nil), ids...)
				return resellers, nil
			},
		),
		routePriceRepositoryFunc(
			func(
				_ context.Context,
				ids []string,
			) (map[string]domain.RoutePrice, error) {
				gotRouteIDs = append([]string(nil), ids...)
				return prices, nil
			},
		),
		routeCandidatePreflightFunc(
			func(
				_ context.Context,
				value RouteCandidatePreflightInput,
			) (RouteCandidatePreflightResult, error) {
				preflightOrder = append(
					preflightOrder,
					value.Route.ID,
				)
				if !bytes.Equal(value.Payload, input.Payload) {
					t.Fatalf(
						"preflight payload = %q, want %q",
						value.Payload,
						input.Payload,
					)
				}
				value.Payload[0] = 'X'
				return validPlannerPreflight(value.Route.ID), nil
			},
		),
		routeCandidateSelectorFunc(
			func(
				_ context.Context,
				value RouteSelectionInput,
			) (RouteSelectionResult, error) {
				selectorInput = value
				return RouteSelectionResult{
					SelectedRouteID:  "route-b",
					FallbackRouteIDs: []string{"route-a"},
				}, nil
			},
		),
	)

	plan, err := planner.Plan(context.Background(), input)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if !reflect.DeepEqual(
		gotResellerIDs,
		[]string{"reseller-a", "reseller-b"},
	) {
		t.Fatalf("reseller ids = %#v", gotResellerIDs)
	}
	if !reflect.DeepEqual(
		gotRouteIDs,
		[]string{"route-a", "route-b"},
	) {
		t.Fatalf("route ids = %#v", gotRouteIDs)
	}
	if !reflect.DeepEqual(
		preflightOrder,
		[]string{"route-a", "route-b"},
	) {
		t.Fatalf("preflight order = %#v", preflightOrder)
	}
	if len(selectorInput.Candidates) != 2 ||
		selectorInput.Candidates[0].Route.ID != "route-a" ||
		selectorInput.Candidates[1].Route.ID != "route-b" {
		t.Fatalf("selector input = %+v", selectorInput)
	}
	if !bytes.Equal(input.Payload, []byte(`{"model":"model-1"}`)) {
		t.Fatalf("caller payload mutated: %q", input.Payload)
	}
	if plan.Route.ID != "route-b" ||
		plan.Reseller.ID != "reseller-b" ||
		plan.Price.RouteID != "route-b" ||
		plan.BillingModel != "openai:provider-route-b" ||
		plan.EstimatedUpstreamCostCents != 20 {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Fallbacks) != 1 ||
		plan.Fallbacks[0].Route.ID != "route-a" ||
		plan.Fallbacks[0].Reseller.ID != "reseller-a" ||
		plan.Fallbacks[0].Price.RouteID != "route-a" ||
		plan.Fallbacks[0].BillingModel !=
			"openai:provider-route-a" ||
		plan.Fallbacks[0].EstimatedUpstreamCostCents != 10 {
		t.Fatalf("fallbacks = %+v", plan.Fallbacks)
	}
}

func TestRepositoryRoutePlannerReturnsUnknownModel(t *testing.T) {
	planner := mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				context.Context,
				ports.RouteQuery,
			) ([]domain.Route, error) {
				return nil, nil
			},
		),
		unusedResellerRepository(t),
		unusedRoutePriceRepository(t),
		unusedRouteCandidatePreflighter(t),
		unusedRouteCandidateSelector(t),
	)

	_, err := planner.Plan(
		context.Background(),
		validRoutePlanInput(),
	)
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("error = %v, want unknown model", err)
	}
}

func TestRepositoryRoutePlannerRejectsRepositoryContractViolation(
	t *testing.T,
) {
	input := validRoutePlanInput()
	route := validPlannerRoute("route-a", "reseller-a")
	route.ClientModel = "other-model"

	planner := mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				context.Context,
				ports.RouteQuery,
			) ([]domain.Route, error) {
				return []domain.Route{route}, nil
			},
		),
		unusedResellerRepository(t),
		unusedRoutePriceRepository(t),
		unusedRouteCandidatePreflighter(t),
		unusedRouteCandidateSelector(t),
	)

	_, err := planner.Plan(context.Background(), input)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestRepositoryRoutePlannerAllowsMissingPriceAsUnavailableCandidate(
	t *testing.T,
) {
	input := validRoutePlanInput()
	route := validPlannerRoute("route-a", "reseller-a")
	reseller := validPlannerReseller("reseller-a")
	var preflightSawNilPrice bool

	planner := mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				context.Context,
				ports.RouteQuery,
			) ([]domain.Route, error) {
				return []domain.Route{route}, nil
			},
		),
		resellerRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.Reseller, error) {
				return map[string]domain.Reseller{
					reseller.ID: reseller,
				}, nil
			},
		),
		routePriceRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.RoutePrice, error) {
				return map[string]domain.RoutePrice{}, nil
			},
		),
		routeCandidatePreflightFunc(
			func(
				_ context.Context,
				value RouteCandidatePreflightInput,
			) (RouteCandidatePreflightResult, error) {
				preflightSawNilPrice = value.Price == nil
				return RouteCandidatePreflightResult{
					SecretAvailable:               true,
					CostAvailable:                 false,
					RateLimitAllowed:              true,
					ConcurrencyAllowed:            true,
					ModelIdentifierRewriteAllowed: true,
				}, nil
			},
		),
		routeCandidateSelectorFunc(
			func(
				context.Context,
				RouteSelectionInput,
			) (RouteSelectionResult, error) {
				return RouteSelectionResult{},
					errors.New("no route available")
			},
		),
	)

	_, err := planner.Plan(context.Background(), input)
	if err == nil || !preflightSawNilPrice {
		t.Fatalf(
			"error = %v, preflight saw nil price = %v",
			err,
			preflightSawNilPrice,
		)
	}
}

func TestRepositoryRoutePlannerRejectsUnknownSelectorRoute(
	t *testing.T,
) {
	planner := plannerWithSingleCandidate(
		t,
		func(
			context.Context,
			RouteSelectionInput,
		) (RouteSelectionResult, error) {
			return RouteSelectionResult{
				SelectedRouteID: "unknown-route",
			}, nil
		},
	)

	_, err := planner.Plan(
		context.Background(),
		validRoutePlanInput(),
	)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestRepositoryRoutePlannerRejectsSelectedRouteAsFallback(
	t *testing.T,
) {
	planner := plannerWithSingleCandidate(
		t,
		func(
			context.Context,
			RouteSelectionInput,
		) (RouteSelectionResult, error) {
			return RouteSelectionResult{
				SelectedRouteID:  "route-a",
				FallbackRouteIDs: []string{"route-a"},
			}, nil
		},
	)

	_, err := planner.Plan(
		context.Background(),
		validRoutePlanInput(),
	)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestRepositoryRoutePlannerRejectsIneligibleSelectedRoute(
	t *testing.T,
) {
	input := validRoutePlanInput()
	route := validPlannerRoute("route-a", "reseller-a")
	reseller := validPlannerReseller("reseller-a")
	price := validPlannerPrice("route-a")

	planner := mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				context.Context,
				ports.RouteQuery,
			) ([]domain.Route, error) {
				return []domain.Route{route}, nil
			},
		),
		resellerRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.Reseller, error) {
				return map[string]domain.Reseller{
					reseller.ID: reseller,
				}, nil
			},
		),
		routePriceRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.RoutePrice, error) {
				return map[string]domain.RoutePrice{
					price.RouteID: price,
				}, nil
			},
		),
		routeCandidatePreflightFunc(
			func(
				context.Context,
				RouteCandidatePreflightInput,
			) (RouteCandidatePreflightResult, error) {
				value := validPlannerPreflight(route.ID)
				value.SecretAvailable = false
				return value, nil
			},
		),
		routeCandidateSelectorFunc(
			func(
				context.Context,
				RouteSelectionInput,
			) (RouteSelectionResult, error) {
				return RouteSelectionResult{
					SelectedRouteID: route.ID,
				}, nil
			},
		),
	)

	_, err := planner.Plan(context.Background(), input)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func TestRepositoryRoutePlannerStopsAtDependencyError(t *testing.T) {
	dependencyError := errors.New("dependency failed")
	input := validRoutePlanInput()
	route := validPlannerRoute("route-a", "reseller-a")

	tests := []struct {
		name    string
		planner *RepositoryRoutePlanner
	}{
		{
			name: "routes",
			planner: mustRepositoryRoutePlanner(
				t,
				routeRepositoryFunc(
					func(
						context.Context,
						ports.RouteQuery,
					) ([]domain.Route, error) {
						return nil, dependencyError
					},
				),
				unusedResellerRepository(t),
				unusedRoutePriceRepository(t),
				unusedRouteCandidatePreflighter(t),
				unusedRouteCandidateSelector(t),
			),
		},
		{
			name: "resellers",
			planner: mustRepositoryRoutePlanner(
				t,
				routeRepositoryFunc(
					func(
						context.Context,
						ports.RouteQuery,
					) ([]domain.Route, error) {
						return []domain.Route{route}, nil
					},
				),
				resellerRepositoryFunc(
					func(
						context.Context,
						[]string,
					) (map[string]domain.Reseller, error) {
						return nil, dependencyError
					},
				),
				unusedRoutePriceRepository(t),
				unusedRouteCandidatePreflighter(t),
				unusedRouteCandidateSelector(t),
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.planner.Plan(
				context.Background(),
				input,
			)
			if !errors.Is(err, dependencyError) {
				t.Fatalf(
					"error = %v, want dependency error",
					err,
				)
			}
		})
	}
}

func plannerWithSingleCandidate(
	t *testing.T,
	selector routeCandidateSelectorFunc,
) *RepositoryRoutePlanner {
	t.Helper()

	route := validPlannerRoute("route-a", "reseller-a")
	reseller := validPlannerReseller("reseller-a")
	price := validPlannerPrice(route.ID)

	return mustRepositoryRoutePlanner(
		t,
		routeRepositoryFunc(
			func(
				context.Context,
				ports.RouteQuery,
			) ([]domain.Route, error) {
				return []domain.Route{route}, nil
			},
		),
		resellerRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.Reseller, error) {
				return map[string]domain.Reseller{
					reseller.ID: reseller,
				}, nil
			},
		),
		routePriceRepositoryFunc(
			func(
				context.Context,
				[]string,
			) (map[string]domain.RoutePrice, error) {
				return map[string]domain.RoutePrice{
					price.RouteID: price,
				}, nil
			},
		),
		routeCandidatePreflightFunc(
			func(
				context.Context,
				RouteCandidatePreflightInput,
			) (RouteCandidatePreflightResult, error) {
				return validPlannerPreflight(route.ID), nil
			},
		),
		selector,
	)
}

func mustRepositoryRoutePlanner(
	t *testing.T,
	routes ports.RouteRepository,
	resellers ports.ResellerQueryRepository,
	prices ports.RoutePriceRepository,
	preflighter RouteCandidatePreflighter,
	selector RouteCandidateSelector,
) *RepositoryRoutePlanner {
	t.Helper()

	planner, err := NewRepositoryRoutePlanner(
		routes,
		resellers,
		prices,
		preflighter,
		selector,
	)
	if err != nil {
		t.Fatalf("NewRepositoryRoutePlanner: %v", err)
	}
	return planner
}

func validRoutePlanInput() RoutePlanInput {
	return RoutePlanInput{
		Principal: Principal{
			UserID:               "user-1",
			APIKeyID:             "key-1",
			BillingSubjectUserID: "billing-1",
		},
		APIFamily:    domain.APIFamilyOpenAICompatible,
		EndpointKind: domain.EndpointChat,
		ClientModel:  "model-1",
		RequestedCapabilities: domain.CapabilitySet{
			Chat: true,
		},
		Payload: []byte(`{"model":"model-1"}`),
	}
}

func validPlannerRoute(
	id string,
	resellerID string,
) domain.Route {
	return domain.Route{
		ID:            id,
		ResellerID:    resellerID,
		ProviderType:  domain.ProviderOpenAI,
		APIFamily:     domain.APIFamilyOpenAICompatible,
		EndpointKind:  domain.EndpointChat,
		ClientModel:   "model-1",
		ProviderModel: "provider-" + id,
		Enabled:       true,
	}
}

func validPlannerReseller(id string) domain.Reseller {
	return domain.Reseller{
		ID:           id,
		ProviderType: domain.ProviderOpenAI,
		Enabled:      true,
	}
}

func validPlannerPrice(routeID string) domain.RoutePrice {
	return domain.RoutePrice{
		RouteID:  routeID,
		Currency: "RUB",
		Enabled:  true,
	}
}

func validPlannerPreflight(
	routeID string,
) RouteCandidatePreflightResult {
	cost := int64(10)
	if routeID == "route-b" {
		cost = 20
	}
	return RouteCandidatePreflightResult{
		ForwardingAdapterAvailable: true,
		SecretAvailable:            true,
		CostAvailable:              true,
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		EstimatedClientAmountCents:    cost + 5,
		EstimatedUpstreamCostCents:    cost,
		Currency:                      "RUB",
		Confidence:                    "conservative",
		RateLimitAllowed:              true,
		ConcurrencyAllowed:            true,
		ModelIdentifierRewriteAllowed: true,
	}
}

func unusedResellerRepository(
	t *testing.T,
) ports.ResellerQueryRepository {
	t.Helper()
	return resellerRepositoryFunc(
		func(
			context.Context,
			[]string,
		) (map[string]domain.Reseller, error) {
			t.Fatal("reseller repository must not be called")
			return nil, nil
		},
	)
}

func unusedRoutePriceRepository(
	t *testing.T,
) ports.RoutePriceRepository {
	t.Helper()
	return routePriceRepositoryFunc(
		func(
			context.Context,
			[]string,
		) (map[string]domain.RoutePrice, error) {
			t.Fatal("route price repository must not be called")
			return nil, nil
		},
	)
}

func unusedRouteCandidatePreflighter(
	t *testing.T,
) RouteCandidatePreflighter {
	t.Helper()
	return routeCandidatePreflightFunc(
		func(
			context.Context,
			RouteCandidatePreflightInput,
		) (RouteCandidatePreflightResult, error) {
			t.Fatal("route preflighter must not be called")
			return RouteCandidatePreflightResult{}, nil
		},
	)
}

func unusedRouteCandidateSelector(
	t *testing.T,
) RouteCandidateSelector {
	t.Helper()
	return routeCandidateSelectorFunc(
		func(
			context.Context,
			RouteSelectionInput,
		) (RouteSelectionResult, error) {
			t.Fatal("route selector must not be called")
			return RouteSelectionResult{}, nil
		},
	)
}
