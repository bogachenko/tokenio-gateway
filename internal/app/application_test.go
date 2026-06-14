package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
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
	ProvisioningInfrastructureGraph,
	BillingInfrastructureGraph,
	ForwardingInfrastructureGraph,
	RepositoryGraph,
) {
	t.Helper()

	cfg := config.Config{
		AdminToken:               "admin-token",
		APIKeyHashSecret:         "api-key-hash-secret",
		ProvisioningServiceToken: "provisioning-service-token",
		APIKeyProvisioningEncryptionKey: bytes.Repeat(
			[]byte{0x42},
			32,
		),
		APIKeyProvisioningKeyVersion: "v1",
		APIKeyProvisioningTTL:        24 * time.Hour,
		CostCurrency:                 "RUB",
		AutoChargeThresholdCents:     1000,
		MinChargeAmountCents:         100,
		RequestBodyMaxBytes:          1024,
		UpstreamResponseBodyMaxBytes: 1024,
	}
	security, err := NewSecurityGraph(cfg)
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	provisioningInfrastructure, err :=
		NewProvisioningInfrastructureGraph(cfg, security)
	if err != nil {
		t.Fatalf(
			"NewProvisioningInfrastructureGraph: %v",
			err,
		)
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
		RouteCapacity: &struct {
			ports.RouteCapacityManager
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

	forwardingInfrastructure, err :=
		NewForwardingInfrastructureGraph()
	if err != nil {
		t.Fatalf(
			"NewForwardingInfrastructureGraph: %v",
			err,
		)
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
		ModelCatalogRoutes: &struct {
			ports.ModelCatalogRouteRepository
		}{},
		RoutePrices: &struct {
			ports.RoutePriceRepository
		}{},
		UsageLedger: &struct {
			ports.UsageLedger
		}{},
		LLMRequestAtomicReservation: &struct {
			llmrequest.AtomicReservation
		}{},
		AdminUsers: &struct {
			ports.AdminUserRepository
		}{},
		AdminAPIKeys: &struct {
			ports.AdminAPIKeyRepository
		}{},
		AdminProvisioning: &struct {
			ports.AdminAPIKeyProvisioningRepository
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

	return cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories
}

func TestNewApplicationGraphWiresExistingPorts(t *testing.T) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)

	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("application graph: %v", err)
	}
	if graph.ModelCatalog == nil {
		t.Fatal("model catalog service is not wired")
	}
	if graph.LLMRequestForwarding == nil {
		t.Fatal("LLM-request forwarding stage is not wired")
	}
	if !graph.ProvisioningEnabled ||
		graph.Provisioning == nil {
		t.Fatal("provisioning service is not wired")
	}
}

func TestNewApplicationGraphRejectsInvalidAutoChargeConfig(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.AutoChargeThresholdCents = 0

	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if err == nil {
		t.Fatal("expected auto-charge configuration error")
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("invalid graph unexpectedly validated")
	}
}

func TestNewApplicationGraphAllowsProvisioningDisabled(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		_,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.ProvisioningServiceToken = ""
	cfg.APIKeyProvisioningEncryptionKey = nil

	security, err := NewSecurityGraph(cfg)
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	provisioningInfrastructure, err :=
		NewProvisioningInfrastructureGraph(cfg, security)
	if err != nil {
		t.Fatalf(
			"NewProvisioningInfrastructureGraph: %v",
			err,
		)
	}
	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("application graph: %v", err)
	}
	if graph.ProvisioningEnabled ||
		graph.Provisioning != nil {
		t.Fatal("disabled provisioning service was constructed")
	}
}

func TestNewApplicationGraphRejectsInvalidProvisioningTTL(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.APIKeyProvisioningTTL = 0

	graph, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if err == nil {
		t.Fatal("expected provisioning TTL error")
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
