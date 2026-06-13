package app

import (
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type applicationGraphClock struct {
	now time.Time
}

func (c applicationGraphClock) Now() time.Time {
	return c.now
}

func validApplicationGraphInputs(
	t *testing.T,
) (
	config.Config,
	RuntimePrimitives,
	SecurityGraph,
	BillingInfrastructureGraph,
	RepositoryGraph,
) {
	t.Helper()

	cfg := config.Config{
		AdminToken:               "admin-token",
		APIKeyHashSecret:         "api-key-hash-secret",
		AutoChargeThresholdCents: 1000,
		MinChargeAmountCents:     100,
	}
	security, err := NewSecurityGraph(cfg)
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}

	primitives := RuntimePrimitives{
		Clock: applicationGraphClock{
			now: time.Date(
				2026,
				time.June,
				13,
				12,
				0,
				0,
				0,
				time.UTC,
			),
		},
		RequestIDs: &struct {
			ports.RequestIDGenerator
		}{},
	}

	billingInfrastructure := BillingInfrastructureGraph{
		Identity: &struct {
			ports.BillingIdentityService
		}{},
		Balance: &struct {
			ports.BillingBalanceClient
		}{},
		Charge: &struct {
			ports.BillingChargeClient
		}{},
	}

	repositories := RepositoryGraph{
		Users: &struct {
			ports.UserRepository
		}{},
		APIKeys: &struct {
			ports.APIKeyRepository
		}{},
		Resellers: &struct {
			ports.ResellerQueryRepository
		}{},
		Routes: &struct {
			ports.RouteRepository
		}{},
		RoutePrices: &struct {
			ports.RoutePriceRepository
		}{},
		UsageLedger: &struct {
			ports.UsageLedger
		}{},
		AdminUsers: &struct {
			ports.AdminUserRepository
		}{},
		AdminAPIKeys: &struct {
			ports.AdminAPIKeyRepository
		}{},
		AdminAudit: &struct {
			ports.AdminAuditStore
		}{},
		AdminResellers: &struct {
			ports.ResellerRepository
		}{},
		AdminRoutes: &struct {
			ports.AdminRouteRepository
		}{},
		AdminRoutePrices: &struct {
			ports.AdminRoutePriceRepository
		}{},
		AdminUsage: &struct {
			ports.AdminUsageLedger
		}{},
		BillingSessions: &struct {
			ports.BillingSessionStore
		}{},
		RouteEvents: &struct {
			ports.RouteEventStore
		}{},
		TelegramAlerts: &struct {
			ports.TelegramAlertStore
		}{},
		APIKeyProvisioning: &struct {
			ports.APIKeyProvisioningStore
		}{},
	}

	return cfg, primitives, security, billingInfrastructure, repositories
}

func TestNewApplicationGraphWiresExistingPorts(t *testing.T) {
	cfg, primitives, security, billingInfrastructure, repositories :=
		validApplicationGraphInputs(t)

	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		billingInfrastructure,
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("application graph: %v", err)
	}
}

func TestNewApplicationGraphRejectsInvalidAutoChargeConfig(
	t *testing.T,
) {
	cfg, primitives, security, billingInfrastructure, repositories :=
		validApplicationGraphInputs(t)
	cfg.AutoChargeThresholdCents = 0

	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		billingInfrastructure,
		repositories,
	)
	if err == nil {
		t.Fatal("expected auto-charge configuration error")
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("invalid graph unexpectedly validated")
	}
}

func TestApplicationGraphValidateRejectsMissingCapability(
	t *testing.T,
) {
	var graph ApplicationGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}
