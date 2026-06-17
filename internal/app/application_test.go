package app

import (
	"bytes"
	"testing"
	"time"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
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
		APIKeyLastUsedTimeout:    250 * time.Millisecond,
		ProvisioningServiceToken: "provisioning-service-token",
		APIKeyProvisioningEncryptionKey: bytes.Repeat(
			[]byte{0x42},
			32,
		),
		APIKeyProvisioningKeyVersion:           "v1",
		APIKeyProvisioningTTL:                  24 * time.Hour,
		APIKeyProvisioningExpirationInterval:   time.Minute,
		APIKeyProvisioningExpirationBatchSize:  100,
		BillingRecoveryInterval:                time.Minute,
		BillingRecoveryBatchSize:               100,
		ForwardingAttemptRecoveryStaleAfter:    5 * time.Minute,
		ForwardingAttemptRecoveryInterval:      time.Minute,
		ForwardingAttemptRecoveryBatchSize:     100,
		TelegramStaleAttemptRecoveryStaleAfter: 5 * time.Minute,
		TelegramStaleAttemptRecoveryInterval:   time.Minute,
		TelegramStaleAttemptRecoveryBatchSize:  100,
		CostCurrency:                           "RUB",
		AutoChargeThresholdCents:               1000,
		MinChargeAmountCents:                   100,
		TokenEstimationSafetyFactor:            1.25,
		CostEstimationSafetyFactor:             1.10,
		RequestBodyMaxBytes:                    1024,
		UpstreamResponseBodyMaxBytes:           1024,
		UpstreamTimeout:                        90 * time.Second,
		UpstreamMaxAttempts:                    3,
		UpstreamMaxBackoff:                     2 * time.Second,
		RateLimitMaxWait:                       5 * time.Second,
		CooldownRateLimit:                      time.Minute,
		CooldownQuotaExceeded:                  24 * time.Hour,
		Cooldown5XX:                            30 * time.Second,
		CooldownTimeout:                        30 * time.Second,
		CooldownAuthError:                      24 * time.Hour,
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
		APIKeyUsageRecorder: &struct {
			ports.APIKeyUsageRecorder
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
		BillingRecovery: &struct {
			ports.BillingRecoveryStore
		}{},
		ForwardingAttempts: &struct {
			ports.ForwardingAttemptStore
		}{},
		RouteCooldowns: &struct {
			ports.RouteCooldownStore
		}{},
		TelegramDeliveryAttempts: &struct {
			ports.TelegramDeliveryAttemptStore
		}{},
		LLMRequestAtomicReservation: &struct {
			llmrequest.AtomicReservation
		}{},
		LLMRequestRouteReservationTransfer: &struct {
			llmrequest.RouteReservationTransfer
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

func TestRepositoryGraphRequiresAPIKeyUsageRecorder(t *testing.T) {
	_, _, _, _, _, _, repositories := validApplicationGraphInputs(t)
	repositories.APIKeyUsageRecorder = nil
	if err := repositories.Validate(); err == nil {
		t.Fatal("expected missing API-key usage recorder error")
	}
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
	if _, ok := graph.PublicAuthentication.(*authenticateapp.UsageRecordingAuthenticator); !ok {
		t.Fatalf(
			"public authentication type = %T, want usage-recording decorator",
			graph.PublicAuthentication,
		)
	}
	if graph.ModelCatalog == nil {
		t.Fatal("model catalog service is not wired")
	}
	if graph.UsageResolver == nil {
		t.Fatal("LLM-request usage resolver is not wired")
	}
	if graph.LLMRequest == nil {
		t.Fatal("LLM-request service is not wired")
	}
	if graph.ForwardingAttemptRecovery == nil {
		t.Fatal(
			"forwarding attempt recovery is not wired",
		)
	}
	if graph.TelegramStaleAttemptRecovery == nil {
		t.Fatal(
			"Telegram stale-attempt recovery is not wired",
		)
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

func TestNewApplicationGraphRejectsInvalidRoutingPolicyConfig(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.UpstreamTimeout = 0

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
		t.Fatal("expected routing policy configuration error")
	}
	if graph.LLMRequest != nil {
		t.Fatal("invalid routing policy unexpectedly produced service")
	}
}
