package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminRouteAndPriceRepositoriesIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	routes, err := NewAdminRouteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := NewAdminRoutePriceRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	audits, err := NewAdminAuditStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	resellerID := "admin-route-reseller-" + suffix
	wrongResellerID := "admin-route-wrong-reseller-" + suffix
	routeID := "admin-route-" + suffix
	rollbackRouteID := "admin-route-rollback-" + suffix
	adminSubject := "admin-route-subject-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_route_prices WHERE route_id IN ($1, $2)",
			routeID,
			rollbackRouteID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_routes WHERE id IN ($1, $2)",
			routeID,
			rollbackRouteID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id IN ($1, $2)",
			resellerID,
			wrongResellerID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_admin_audit_log WHERE admin_subject = $1",
			adminSubject,
		)
	})

	insertAdminRouteTestReseller(
		t,
		db,
		resellerID,
		domain.ProviderOpenAI,
		now,
	)
	insertAdminRouteTestReseller(
		t,
		db,
		wrongResellerID,
		domain.ProviderGroq,
		now,
	)

	requested := domain.Route{
		ID:                     routeID,
		ResellerID:             resellerID,
		ProviderType:           domain.ProviderOpenAI,
		APIFamily:              domain.APIFamilyOpenAICompatible,
		EndpointKind:           domain.EndpointChat,
		ClientModel:            "client-" + suffix,
		ProviderModel:          "provider-" + suffix,
		ModelRewritePolicy:     domain.ModelRewritePolicyNone,
		Enabled:                true,
		Priority:               10,
		RequestsPerMinute:      100,
		TokensPerMinute:        1000,
		ConcurrentRequests:     5,
		DefaultMaxOutputTokens: 4096,
		Capabilities:           domain.CapabilitySet{Chat: true, Tools: true},
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	createAudit := adminRouteAuditContext(
		"audit-route-create-"+suffix,
		adminSubject,
		domain.AuditActionRouteCreate,
		routeID,
		domain.AuditState{},
		adminRouteApplicationState(requested),
		"admreq-route-create-"+suffix,
		now,
	)
	created, err := routes.CreateRouteWithAudit(
		ctx,
		requested,
		createAudit,
	)
	if err != nil {
		t.Fatalf("CreateRouteWithAudit: %v", err)
	}
	if !sameAdminRoute(created, requested) {
		t.Fatalf("created route = %+v", created)
	}

	updateAt := now.Add(time.Second)
	updatedInput := created
	updatedInput.ProviderModel = "provider-updated-" + suffix
	updatedInput.Priority = 20
	updatedInput.UpdatedAt = updateAt
	updateAudit := adminRouteAuditContext(
		"audit-route-update-"+suffix,
		adminSubject,
		domain.AuditActionRouteUpdate,
		routeID,
		adminRouteApplicationState(created),
		adminRouteApplicationState(updatedInput),
		"admreq-route-update-"+suffix,
		updateAt,
	)
	updated, err := routes.CompareAndSwapRouteWithAudit(
		ctx,
		created,
		updatedInput,
		updateAudit,
	)
	if err != nil {
		t.Fatalf("update route: %v", err)
	}

	cooldownAt := now.Add(2 * time.Second)
	cooldownUntil := cooldownAt.Add(time.Hour)
	cooldownInput := updated
	cooldownInput.CooldownUntil = &cooldownUntil
	cooldownInput.CooldownReason = "rate_limited"
	cooldownInput.UpdatedAt = cooldownAt
	cooldownAudit := adminRouteAuditContext(
		"audit-route-cooldown-"+suffix,
		adminSubject,
		domain.AuditActionRouteCooldownSet,
		routeID,
		adminRouteApplicationState(updated),
		adminRouteApplicationState(cooldownInput),
		"admreq-route-cooldown-"+suffix,
		cooldownAt,
	)
	cooled, err := routes.CompareAndSwapRouteWithAudit(
		ctx,
		updated,
		cooldownInput,
		cooldownAudit,
	)
	if err != nil {
		t.Fatalf("set cooldown: %v", err)
	}
	if cooled.CooldownUntil == nil ||
		cooled.CooldownReason != "rate_limited" {
		t.Fatalf("cooled route = %+v", cooled)
	}

	staleNext := updated
	staleNext.Priority++
	staleNext.UpdatedAt = now.Add(3 * time.Second)
	staleAudit := adminRouteAuditContext(
		"audit-route-stale-"+suffix,
		adminSubject,
		domain.AuditActionRouteUpdate,
		routeID,
		adminRouteApplicationState(updated),
		adminRouteApplicationState(staleNext),
		"admreq-route-stale-"+suffix,
		staleNext.UpdatedAt,
	)
	_, err = routes.CompareAndSwapRouteWithAudit(
		ctx,
		updated,
		staleNext,
		staleAudit,
	)
	if !errors.Is(err, ports.ErrAdminStateConflict) {
		t.Fatalf("stale route error = %v, want state conflict", err)
	}

	page, err := routes.ListRoutes(
		ctx,
		ports.RouteListFilter{
			ResellerID:   resellerID,
			ProviderType: domain.ProviderOpenAI,
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  requested.ClientModel,
			Enabled:      adminRouteBoolPointer(true),
			Page:         ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	if page.Total != 1 ||
		len(page.Items) != 1 ||
		page.Items[0].ID != routeID {
		t.Fatalf("route page = %+v", page)
	}

	priceCreatedAt := now.Add(4 * time.Second)
	const createMarkup = 1.2345678901234567
	const updateMarkup = 1.9876543210987654
	price := domain.RoutePrice{
		RouteID:                     routeID,
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  10,
		OutputPricePer1MTokensCents: 20,
		ImageGenerationUnitKind:     domain.ImageGenerationUnitKindNone,
		MarkupCoefficient:           createMarkup,
		Enabled:                     true,
		CreatedAt:                   priceCreatedAt,
		UpdatedAt:                   priceCreatedAt,
	}
	createPriceAudit := adminRoutePriceAuditContext(
		"audit-price-create-"+suffix,
		adminSubject,
		routeID,
		domain.AuditState{},
		adminRoutePriceApplicationState(price),
		"admreq-price-create-"+suffix,
		priceCreatedAt,
	)
	createdPrice, err := prices.UpsertRoutePriceWithAudit(
		ctx,
		nil,
		price,
		createPriceAudit,
	)
	if err != nil {
		t.Fatalf("create route price: %v", err)
	}
	if createdPrice.MarkupCoefficient != createMarkup {
		t.Fatalf(
			"created markup=%0.17g want=%0.17g",
			createdPrice.MarkupCoefficient,
			createMarkup,
		)
	}
	assertRoutePriceMarkupPersistence(
		t,
		ctx,
		db,
		prices,
		routeID,
		createPriceAudit.ID,
		createMarkup,
	)

	priceUpdateAt := now.Add(5 * time.Second)
	nextPrice := createdPrice
	nextPrice.OutputPricePer1MTokensCents = 30
	nextPrice.MarkupCoefficient = updateMarkup
	nextPrice.UpdatedAt = priceUpdateAt
	updatePriceAudit := adminRoutePriceAuditContext(
		"audit-price-update-"+suffix,
		adminSubject,
		routeID,
		adminRoutePriceApplicationState(createdPrice),
		adminRoutePriceApplicationState(nextPrice),
		"admreq-price-update-"+suffix,
		priceUpdateAt,
	)
	updatedPrice, err := prices.UpsertRoutePriceWithAudit(
		ctx,
		&createdPrice,
		nextPrice,
		updatePriceAudit,
	)
	if err != nil {
		t.Fatalf("update route price: %v", err)
	}
	if updatedPrice.OutputPricePer1MTokensCents != 30 ||
		updatedPrice.MarkupCoefficient != updateMarkup {
		t.Fatalf("updated price = %+v", updatedPrice)
	}
	assertRoutePriceMarkupPersistence(
		t,
		ctx,
		db,
		prices,
		routeID,
		updatePriceAudit.ID,
		updateMarkup,
	)

	stalePrice := createdPrice
	stalePrice.OutputPricePer1MTokensCents = 40
	stalePrice.UpdatedAt = now.Add(6 * time.Second)
	stalePriceAudit := adminRoutePriceAuditContext(
		"audit-price-stale-"+suffix,
		adminSubject,
		routeID,
		adminRoutePriceApplicationState(createdPrice),
		adminRoutePriceApplicationState(stalePrice),
		"admreq-price-stale-"+suffix,
		stalePrice.UpdatedAt,
	)
	_, err = prices.UpsertRoutePriceWithAudit(
		ctx,
		&createdPrice,
		stalePrice,
		stalePriceAudit,
	)
	if !errors.Is(err, ports.ErrAdminStateConflict) {
		t.Fatalf("stale price error = %v, want state conflict", err)
	}

	wrongRoute := requested
	wrongRoute.ID = rollbackRouteID
	wrongRoute.ResellerID = wrongResellerID
	wrongRoute.CreatedAt = now.Add(7 * time.Second)
	wrongRoute.UpdatedAt = wrongRoute.CreatedAt
	wrongRouteAudit := adminRouteAuditContext(
		"audit-route-wrong-provider-"+suffix,
		adminSubject,
		domain.AuditActionRouteCreate,
		rollbackRouteID,
		domain.AuditState{},
		adminRouteApplicationState(wrongRoute),
		"admreq-route-wrong-provider-"+suffix,
		wrongRoute.CreatedAt,
	)
	_, err = routes.CreateRouteWithAudit(
		ctx,
		wrongRoute,
		wrongRouteAudit,
	)
	if !errors.Is(err, ports.ErrAdminConflict) {
		t.Fatalf("provider mismatch error = %v, want conflict", err)
	}

	rollbackRoute := requested
	rollbackRoute.ID = rollbackRouteID
	rollbackRoute.CreatedAt = now.Add(8 * time.Second)
	rollbackRoute.UpdatedAt = rollbackRoute.CreatedAt
	rollbackAudit := adminRouteAuditContext(
		createAudit.ID,
		adminSubject,
		domain.AuditActionRouteCreate,
		rollbackRouteID,
		domain.AuditState{},
		adminRouteApplicationState(rollbackRoute),
		"admreq-route-rollback-"+suffix,
		rollbackRoute.CreatedAt,
	)
	_, err = routes.CreateRouteWithAudit(
		ctx,
		rollbackRoute,
		rollbackAudit,
	)
	if !errors.Is(err, ports.ErrAdminConflict) {
		t.Fatalf("audit collision error = %v, want conflict", err)
	}
	_, err = routes.FindRouteByID(ctx, rollbackRouteID)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("rolled-back route error = %v, want not found", err)
	}

	auditPage, err := audits.ListAuditEntries(
		ctx,
		ports.AuditListFilter{
			AdminSubject: adminSubject,
			Page:         ports.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if auditPage.Total != 5 || len(auditPage.Items) != 5 {
		t.Fatalf("audit page = %+v", auditPage)
	}
}

func assertRoutePriceMarkupPersistence(
	t *testing.T,
	ctx context.Context,
	db *DB,
	prices *AdminRoutePriceRepository,
	routeID string,
	auditID string,
	want float64,
) {
	t.Helper()

	found, err := prices.FindRoutePrice(ctx, routeID)
	if err != nil {
		t.Fatalf("FindRoutePrice: %v", err)
	}
	if found.MarkupCoefficient != want {
		t.Fatalf(
			"repository markup=%0.17g want=%0.17g",
			found.MarkupCoefficient,
			want,
		)
	}

	var persisted float64
	if err := db.QueryRow(
		ctx,
		`
SELECT markup_coefficient
FROM tokenio_route_prices
WHERE route_id = $1
`,
		routeID,
	).Scan(&persisted); err != nil {
		t.Fatalf("read persisted markup: %v", err)
	}
	if persisted != want {
		t.Fatalf(
			"persisted markup=%0.17g want=%0.17g",
			persisted,
			want,
		)
	}

	var audited float64
	if err := db.QueryRow(
		ctx,
		`
SELECT (after_state ->> 'markup_coefficient')::double precision
FROM tokenio_admin_audit_log
WHERE id = $1
`,
		auditID,
	).Scan(&audited); err != nil {
		t.Fatalf("read audited markup: %v", err)
	}
	if audited != want {
		t.Fatalf(
			"audit markup=%0.17g want=%0.17g",
			audited,
			want,
		)
	}
}

func insertAdminRouteTestReseller(
	t *testing.T,
	db *DB,
	id string,
	providerType domain.ProviderType,
	now time.Time,
) {
	t.Helper()
	if _, err := db.Exec(
		t.Context(),
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
VALUES ($1, 'route-test', $2, 'https://example.test', $3, TRUE, 1000, 0, 0, $4, $4)
`,
		id,
		string(providerType),
		"ROUTE_TEST_KEY_"+id,
		now,
	); err != nil {
		t.Fatalf("insert reseller: %v", err)
	}
}

func adminRouteAuditContext(
	id string,
	adminSubject string,
	action domain.AuditAction,
	entityID string,
	before domain.AuditState,
	after domain.AuditState,
	requestID string,
	createdAt time.Time,
) domain.AuditContext {
	return domain.AuditContext{
		ID:           id,
		AdminSubject: adminSubject,
		Action:       action,
		EntityType:   "route",
		EntityID:     entityID,
		BeforeState:  before,
		AfterState:   after,
		RequestID:    requestID,
		CreatedAt:    createdAt,
	}
}

func adminRoutePriceAuditContext(
	id string,
	adminSubject string,
	entityID string,
	before domain.AuditState,
	after domain.AuditState,
	requestID string,
	createdAt time.Time,
) domain.AuditContext {
	return domain.AuditContext{
		ID:           id,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionRoutePriceUpsert,
		EntityType:   "route_price",
		EntityID:     entityID,
		BeforeState:  before,
		AfterState:   after,
		RequestID:    requestID,
		CreatedAt:    createdAt,
	}
}

func adminRouteBoolPointer(value bool) *bool {
	return &value
}
