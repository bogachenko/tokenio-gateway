package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fakeRoutes struct {
	values []domain.Route
	err    error
	calls  int
	family domain.APIFamily
}

func (f *fakeRoutes) ListModelCatalogRoutes(
	_ context.Context,
	family domain.APIFamily,
) ([]domain.Route, error) {
	f.calls++
	f.family = family
	return append([]domain.Route(nil), f.values...), f.err
}

type fakeResellers struct {
	values map[string]domain.Reseller
	err    error
	ids    []string
}

func (f *fakeResellers) FindByIDs(
	_ context.Context,
	ids []string,
) (map[string]domain.Reseller, error) {
	f.ids = append([]string(nil), ids...)
	return f.values, f.err
}

type fakePrices struct {
	values map[string]domain.RoutePrice
	err    error
	ids    []string
}

func (f *fakePrices) FindByRouteIDs(
	_ context.Context,
	ids []string,
) (map[string]domain.RoutePrice, error) {
	f.ids = append([]string(nil), ids...)
	return f.values, f.err
}

type fakeSecrets struct {
	values map[string]bool
	err    error
	calls  map[string]int
}

func (f *fakeSecrets) Exists(
	_ context.Context,
	name string,
) (bool, error) {
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[name]++
	return f.values[name], f.err
}

type fakeRewriteSupport struct {
	supported bool
	calls     int
}

func (*fakeRewriteSupport) SupportsForwardingAdapter(
	domain.APIFamily,
	domain.ProviderType,
) bool {
	return true
}

func (f *fakeRewriteSupport) SupportsModelIdentifierRewrite(
	domain.APIFamily,
	domain.ProviderType,
) bool {
	f.calls++
	return f.supported
}

type fixedClock struct {
	value time.Time
}

func (f fixedClock) Now() time.Time {
	return f.value
}

func testTime() time.Time {
	return time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}

func testRoute(
	id string,
	model string,
	resellerID string,
	priority int,
) domain.Route {
	return domain.Route{
		ID:                 id,
		ResellerID:         resellerID,
		ProviderType:       domain.ProviderOpenAI,
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        model,
		ProviderModel:      model,
		ModelRewritePolicy: domain.ModelRewritePolicyNone,
		Enabled:            true,
		Priority:           priority,
		Capabilities: domain.CapabilitySet{
			Chat: true,
		},
	}
}

func testReseller(
	id string,
	secretName string,
) domain.Reseller {
	return domain.Reseller{
		ID:                  id,
		Name:                id,
		ProviderType:        domain.ProviderOpenAI,
		BaseURL:             "https://" + id + ".example.test",
		APIKeyEnv:           secretName,
		Enabled:             true,
		BalanceCents:        100000,
		ReservedCents:       100,
		MinimumBalanceCents: 1000,
	}
}

func testPrice(
	routeID string,
	input int64,
	output int64,
	markup float64,
) domain.RoutePrice {
	return domain.RoutePrice{
		RouteID:                     routeID,
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  input,
		OutputPricePer1MTokensCents: output,
		ImageGenerationUnitKind:     domain.ImageGenerationUnitKindNone,
		MarkupCoefficient:           markup,
		Enabled:                     true,
	}
}

func newTestService(
	t *testing.T,
	routes *fakeRoutes,
	resellers *fakeResellers,
	prices *fakePrices,
	secrets *fakeSecrets,
	rewrite *fakeRewriteSupport,
) *Service {
	t.Helper()
	service, err := NewService(Dependencies{
		Routes:         routes,
		Resellers:      resellers,
		Prices:         prices,
		Secrets:        secrets,
		AdapterSupport: rewrite,
		RewriteSupport: rewrite,
		Clock: fixedClock{
			value: testTime(),
		},
		Currency: "RUB",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func TestListBuildsSafeDeterministicCatalog(
	t *testing.T,
) {
	routeExpensive := testRoute(
		"route_expensive",
		"gpt-test",
		"reseller_expensive",
		20,
	)
	routeExpensive.Capabilities.Tools = true

	routeSelected := testRoute(
		"route_selected",
		"gpt-test",
		"reseller_selected",
		10,
	)
	routeSelected.Capabilities.ImageInput = true

	routeInactive := testRoute(
		"route_inactive",
		"aaa-inactive",
		"reseller_inactive",
		1,
	)
	routeInactive.Enabled = false
	disabledAt := testTime().Add(-time.Hour)
	routeInactive.DisabledAt = &disabledAt

	routes := &fakeRoutes{
		values: []domain.Route{
			routeExpensive,
			routeInactive,
			routeSelected,
		},
	}
	resellers := &fakeResellers{
		values: map[string]domain.Reseller{
			"reseller_expensive": testReseller(
				"reseller_expensive",
				"EXPENSIVE_KEY",
			),
			"reseller_selected": testReseller(
				"reseller_selected",
				"SELECTED_KEY",
			),
			"reseller_inactive": testReseller(
				"reseller_inactive",
				"INACTIVE_KEY",
			),
		},
	}
	prices := &fakePrices{
		values: map[string]domain.RoutePrice{
			"route_expensive": testPrice(
				"route_expensive",
				200,
				400,
				1,
			),
			"route_selected": testPrice(
				"route_selected",
				100,
				200,
				1.5,
			),
			"route_inactive": testPrice(
				"route_inactive",
				10,
				20,
				1,
			),
		},
	}
	secrets := &fakeSecrets{
		values: map[string]bool{
			"EXPENSIVE_KEY": true,
			"SELECTED_KEY":  true,
			"INACTIVE_KEY":  true,
		},
	}
	service := newTestService(
		t,
		routes,
		resellers,
		prices,
		secrets,
		&fakeRewriteSupport{},
	)

	catalog, err := service.List(
		context.Background(),
		domain.APIFamilyOpenAICompatible,
	)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if catalog.Object != "list" ||
		len(catalog.Data) != 2 ||
		catalog.Data[0].ID != "aaa-inactive" ||
		catalog.Data[1].ID != "gpt-test" {
		t.Fatalf("catalog = %+v", catalog)
	}

	inactive := catalog.Data[0]
	if inactive.Active ||
		inactive.Pricing != nil ||
		inactive.Capabilities !=
			(domain.CapabilitySet{}) {
		t.Fatalf("inactive model = %+v", inactive)
	}

	active := catalog.Data[1]
	if !active.Active ||
		active.Object != "model" ||
		active.OwnedBy != "tokenio" ||
		active.Type != "chat" ||
		active.Pricing == nil ||
		active.Pricing.InputPricePer1MTokensCents != 150 ||
		active.Pricing.OutputPricePer1MTokensCents != 300 ||
		!active.Capabilities.Chat ||
		!active.Capabilities.Tools ||
		!active.Capabilities.ImageInput {
		t.Fatalf("active model = %+v", active)
	}

	if secrets.calls["EXPENSIVE_KEY"] != 1 ||
		secrets.calls["SELECTED_KEY"] != 1 ||
		secrets.calls["INACTIVE_KEY"] != 0 {
		t.Fatalf("secret calls = %+v", secrets.calls)
	}

	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	response := string(encoded)
	for _, forbidden := range []string{
		"route_id",
		"reseller_id",
		"provider_model",
		"provider_type",
		"api_key_env",
		"markup_coefficient",
		"cooldown_reason",
		"priority",
	} {
		if strings.Contains(response, forbidden) {
			t.Fatalf(
				"catalog leaked %q: %s",
				forbidden,
				response,
			)
		}
	}
}

func TestListUsesPriorityAndRouteIDAsReferenceCostTieBreakers(
	t *testing.T,
) {
	routeA := testRoute(
		"route_a",
		"gpt-tie",
		"reseller_a",
		20,
	)
	routeB := testRoute(
		"route_b",
		"gpt-tie",
		"reseller_b",
		10,
	)

	service := newTestService(
		t,
		&fakeRoutes{
			values: []domain.Route{
				routeA,
				routeB,
			},
		},
		&fakeResellers{
			values: map[string]domain.Reseller{
				"reseller_a": testReseller(
					"reseller_a",
					"A_KEY",
				),
				"reseller_b": testReseller(
					"reseller_b",
					"B_KEY",
				),
			},
		},
		&fakePrices{
			values: map[string]domain.RoutePrice{
				"route_a": testPrice(
					"route_a",
					100,
					300,
					1,
				),
				"route_b": testPrice(
					"route_b",
					200,
					200,
					1,
				),
			},
		},
		&fakeSecrets{
			values: map[string]bool{
				"A_KEY": true,
				"B_KEY": true,
			},
		},
		&fakeRewriteSupport{},
	)

	catalog, err := service.List(
		context.Background(),
		domain.APIFamilyOpenAICompatible,
	)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	pricing := catalog.Data[0].Pricing
	if pricing == nil ||
		pricing.InputPricePer1MTokensCents != 200 ||
		pricing.OutputPricePer1MTokensCents != 200 {
		t.Fatalf("selected pricing = %+v", pricing)
	}
}

func TestListRequiresExplicitModelRewriteSupport(
	t *testing.T,
) {
	route := testRoute(
		"route_rewrite",
		"gpt-rewrite",
		"reseller_rewrite",
		1,
	)
	route.ProviderModel = "provider/gpt-rewrite"
	route.ModelRewritePolicy =
		domain.ModelRewritePolicyProviderModel

	for _, test := range []struct {
		name      string
		supported bool
		active    bool
	}{
		{
			name:      "unsupported",
			supported: false,
			active:    false,
		},
		{
			name:      "supported",
			supported: true,
			active:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rewrite := &fakeRewriteSupport{
				supported: test.supported,
			}
			service := newTestService(
				t,
				&fakeRoutes{
					values: []domain.Route{route},
				},
				&fakeResellers{
					values: map[string]domain.Reseller{
						"reseller_rewrite": testReseller(
							"reseller_rewrite",
							"REWRITE_KEY",
						),
					},
				},
				&fakePrices{
					values: map[string]domain.RoutePrice{
						"route_rewrite": testPrice(
							"route_rewrite",
							100,
							200,
							1,
						),
					},
				},
				&fakeSecrets{
					values: map[string]bool{
						"REWRITE_KEY": true,
					},
				},
				rewrite,
			)

			catalog, err := service.List(
				context.Background(),
				domain.APIFamilyOpenAICompatible,
			)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if catalog.Data[0].Active != test.active ||
				rewrite.calls != 1 {
				t.Fatalf(
					"model=%+v rewrite calls=%d",
					catalog.Data[0],
					rewrite.calls,
				)
			}
		})
	}
}

func TestListMarksMissingSecretOrBalanceInactive(
	t *testing.T,
) {
	for _, test := range []struct {
		name     string
		secret   bool
		balance  int64
		reserved int64
		minimum  int64
	}{
		{
			name:    "missing secret",
			secret:  false,
			balance: 100,
		},
		{
			name:     "no available balance",
			secret:   true,
			balance:  100,
			reserved: 50,
			minimum:  50,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			route := testRoute(
				"route_1",
				"gpt-test",
				"reseller_1",
				1,
			)
			reseller := testReseller(
				"reseller_1",
				"ROUTE_KEY",
			)
			reseller.BalanceCents = test.balance
			reseller.ReservedCents = test.reserved
			reseller.MinimumBalanceCents =
				test.minimum

			service := newTestService(
				t,
				&fakeRoutes{
					values: []domain.Route{route},
				},
				&fakeResellers{
					values: map[string]domain.Reseller{
						"reseller_1": reseller,
					},
				},
				&fakePrices{
					values: map[string]domain.RoutePrice{
						"route_1": testPrice(
							"route_1",
							100,
							200,
							1,
						),
					},
				},
				&fakeSecrets{
					values: map[string]bool{
						"ROUTE_KEY": test.secret,
					},
				},
				&fakeRewriteSupport{},
			)

			catalog, err := service.List(
				context.Background(),
				domain.APIFamilyOpenAICompatible,
			)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if catalog.Data[0].Active ||
				catalog.Data[0].Pricing != nil {
				t.Fatalf(
					"model = %+v",
					catalog.Data[0],
				)
			}
		})
	}
}

func TestListRejectsMixedEndpointKindsForOneModel(
	t *testing.T,
) {
	chat := testRoute(
		"route_chat",
		"mixed-model",
		"reseller_1",
		1,
	)
	embeddings := testRoute(
		"route_embeddings",
		"mixed-model",
		"reseller_1",
		2,
	)
	embeddings.EndpointKind = domain.EndpointEmbeddings
	embeddings.Capabilities =
		domain.CapabilitySet{Embeddings: true}

	service := newTestService(
		t,
		&fakeRoutes{
			values: []domain.Route{
				chat,
				embeddings,
			},
		},
		&fakeResellers{},
		&fakePrices{},
		&fakeSecrets{},
		&fakeRewriteSupport{},
	)

	_, err := service.List(
		context.Background(),
		domain.APIFamilyOpenAICompatible,
	)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf(
			"error = %v, want ErrCatalogUnavailable",
			err,
		)
	}
}

func TestListMapsDependencyErrorsWithoutLeakage(
	t *testing.T,
) {
	service := newTestService(
		t,
		&fakeRoutes{
			err: errors.New(
				"postgres password sk_live_secret",
			),
		},
		&fakeResellers{},
		&fakePrices{},
		&fakeSecrets{},
		&fakeRewriteSupport{},
	)

	_, err := service.List(
		context.Background(),
		domain.APIFamilyOpenAICompatible,
	)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf(
			"error = %v, want ErrCatalogUnavailable",
			err,
		)
	}
	if strings.Contains(err.Error(), "sk_live_") {
		t.Fatalf("dependency error leaked: %v", err)
	}
}

func TestApplyMarkupIsCeilingAndRejectsOverflow(
	t *testing.T,
) {
	value, err := applyMarkup(101, 1.5)
	if err != nil || value != 152 {
		t.Fatalf("value=%d error=%v", value, err)
	}

	decimalValue, err := applyMarkup(100, 1.1)
	if err != nil || decimalValue != 110 {
		t.Fatalf(
			"decimal value=%d error=%v",
			decimalValue,
			err,
		)
	}

	_, err = applyMarkup(math.MaxInt64, 2)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf(
			"overflow error = %v",
			err,
		)
	}
}

func TestNewServiceRejectsMissingDependencies(
	t *testing.T,
) {
	service, err := NewService(Dependencies{})
	if service != nil ||
		!errors.Is(err, ErrInvalidInput) {
		t.Fatalf(
			"service=%v error=%v",
			service,
			err,
		)
	}
}

var _ ports.ModelCatalogRouteRepository = (*fakeRoutes)(nil)
var _ ports.ResellerQueryRepository = (*fakeResellers)(nil)
var _ ports.RoutePriceRepository = (*fakePrices)(nil)
var _ ports.SecretPresenceChecker = (*fakeSecrets)(nil)
var (
	_ ports.ForwardingAdapterSupport      = (*fakeRewriteSupport)(nil)
	_ ports.ModelIdentifierRewriteSupport = (*fakeRewriteSupport)(nil)
)
