package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestRegistryReadRepositoriesIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "test-user-" + suffix
	keyID := "test-key-" + suffix
	keyHash := "test-hash-" + suffix
	resellerID := "test-reseller-" + suffix
	routeID := "test-route-" + suffix
	clientModel := "test-model-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)
	markup := 1.23456789012345

	t.Cleanup(func() {
		for _, statement := range []string{
			"DELETE FROM tokenio_route_prices WHERE route_id = $1",
			"DELETE FROM tokenio_routes WHERE id = $1",
			"DELETE FROM tokenio_resellers WHERE id = $1",
			"DELETE FROM tokenio_api_keys WHERE id = $1",
			"DELETE FROM tokenio_users WHERE id = $1",
		} {
			var argument string
			switch statement {
			case "DELETE FROM tokenio_route_prices WHERE route_id = $1",
				"DELETE FROM tokenio_routes WHERE id = $1":
				argument = routeID
			case "DELETE FROM tokenio_resellers WHERE id = $1":
				argument = resellerID
			case "DELETE FROM tokenio_api_keys WHERE id = $1":
				argument = keyID
			default:
				argument = userID
			}
			_, _ = db.Exec(context.Background(), statement, argument)
		}
	})

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, TRUE, $5, $5)
`,
		userID,
		"billing-"+suffix,
		"user-"+suffix+"@example.test",
		"Registry Test",
		now,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_api_keys (
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    enabled,
    created_at,
    updated_at
)
VALUES ($1, $2, 'integration', $3, 'sk_test', TRUE, $4, $4)
`,
		keyID,
		userID,
		keyHash,
		now,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at
)
VALUES ($1, 'integration', 'openai', 'https://example.test', $2, TRUE, 10000, 100, 500, $3, $3)
`,
		resellerID,
		"TEST_RESELLER_KEY_"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert reseller: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_routes (
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    model_rewrite_policy,
    enabled,
    priority,
    requests_per_minute,
    tokens_per_minute,
    concurrent_requests,
    default_max_output_tokens,
    capabilities,
    created_at,
    updated_at
)
VALUES (
    $1,
    $2,
    'openai',
    'openai_compatible',
    'chat',
    $3,
    $3,
    'none',
    TRUE,
    10,
    100,
    100000,
    5,
    4096,
    $4::jsonb,
    $5,
    $5
)
`,
		routeID,
		resellerID,
		clientModel,
		`{"chat":true,"tools":true}`,
		now,
	); err != nil {
		t.Fatalf("insert route: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_route_prices (
    route_id,
    currency,
    input_price_per_1m_tokens_cents,
    output_price_per_1m_tokens_cents,
    image_generation_unit_kind,
    markup_coefficient,
    enabled,
    created_at,
    updated_at
)
VALUES ($1, 'RUB', 100, 200, 'none', $2, TRUE, $3, $3)
`,
		routeID,
		markup,
		now,
	); err != nil {
		t.Fatalf("insert route price: %v", err)
	}

	userRepository, err := NewUserRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	apiKeyRepository, err := NewAPIKeyRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	resellerRepository, err := NewResellerRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	routeRepository, err := NewRouteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	priceRepository, err := NewRoutePriceRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	user, err := userRepository.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if user.ID != userID || user.ExternalBillingUserID != "billing-"+suffix {
		t.Fatalf("user = %+v", user)
	}

	key, err := apiKeyRepository.FindByHash(ctx, keyHash)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if key.ID != keyID || key.UserID != userID || key.KeyHash != keyHash {
		t.Fatalf("api key = %+v", key)
	}

	resellers, err := resellerRepository.FindByIDs(
		ctx,
		[]string{resellerID, resellerID},
	)
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	reseller, ok := resellers[resellerID]
	if !ok || reseller.ProviderType != domain.ProviderOpenAI {
		t.Fatalf("resellers = %+v", resellers)
	}

	routes, err := routeRepository.FindRoutes(ctx, ports.RouteQuery{
		APIFamily:    domain.APIFamilyOpenAICompatible,
		EndpointKind: domain.EndpointChat,
		ClientModel:  clientModel,
	})
	if err != nil {
		t.Fatalf("FindRoutes: %v", err)
	}
	if len(routes) != 1 ||
		routes[0].ID != routeID ||
		!routes[0].Capabilities.Chat ||
		!routes[0].Capabilities.Tools {
		t.Fatalf("routes = %+v", routes)
	}

	catalogRoutes, err :=
		routeRepository.ListModelCatalogRoutes(
			ctx,
			domain.APIFamilyOpenAICompatible,
		)
	if err != nil {
		t.Fatalf(
			"ListModelCatalogRoutes: %v",
			err,
		)
	}
	var catalogRoute *domain.Route
	for index := range catalogRoutes {
		if catalogRoutes[index].ID == routeID {
			catalogRoute = &catalogRoutes[index]
			break
		}
	}
	if catalogRoute == nil ||
		catalogRoute.ClientModel != clientModel ||
		catalogRoute.EndpointKind !=
			domain.EndpointChat ||
		!catalogRoute.Capabilities.Chat {
		t.Fatalf(
			"catalog routes = %+v",
			catalogRoutes,
		)
	}

	prices, err := priceRepository.FindByRouteIDs(
		ctx,
		[]string{routeID, routeID},
	)
	if err != nil {
		t.Fatalf("FindByRouteIDs: %v", err)
	}
	price, ok := prices[routeID]
	if !ok ||
		price.MarkupCoefficient != markup ||
		price.InputPricePer1MTokensCents != 100 ||
		price.OutputPricePer1MTokensCents != 200 {
		t.Fatalf("prices = %+v", prices)
	}

	_, err = userRepository.FindByID(ctx, "missing-"+suffix)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("missing user error = %v, want ErrNotFound", err)
	}

	noRoutes, err := routeRepository.FindRoutes(ctx, ports.RouteQuery{
		APIFamily:    domain.APIFamilyOpenAICompatible,
		EndpointKind: domain.EndpointChat,
		ClientModel:  "missing-" + suffix,
	})
	if err != nil {
		t.Fatalf("FindRoutes missing: %v", err)
	}
	if len(noRoutes) != 0 {
		t.Fatalf("missing routes = %+v", noRoutes)
	}

}
