package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func buildTransportGraph(
	t *testing.T,
	cfg config.Config,
	security SecurityGraph,
	provisioningInfrastructure ProvisioningInfrastructureGraph,
	billingInfrastructure BillingInfrastructureGraph,
	forwardingInfrastructure ForwardingInfrastructureGraph,
	repositories RepositoryGraph,
) TransportGraph {
	t.Helper()

	primitives, err := NewRuntimePrimitives()
	if err != nil {
		t.Fatalf("NewRuntimePrimitives: %v", err)
	}
	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		TelegramInfrastructureGraph{}, validLoggingGraph(t),

		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}
	graph, err := NewTransportGraph(cfg, primitives, security, applications)
	if err != nil {
		t.Fatalf("NewTransportGraph: %v", err)
	}
	return graph
}

func TestNewTransportGraphWiresControlPlanes(t *testing.T) {
	cfg, _, security, provisioningInfrastructure, billingInfrastructure, forwardingInfrastructure, repositories :=
		validApplicationGraphInputs(t)

	graph := buildTransportGraph(
		t,
		cfg,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if err := graph.Validate(); err != nil {
		t.Fatalf("transport graph: %v", err)
	}
	if graph.Public == nil {
		t.Fatal("public API HTTP handler is not wired")
	}
	if !graph.ProvisioningEnabled || graph.Provisioning == nil {
		t.Fatal("provisioning HTTP handler is not wired")
	}

	public := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		public,
		httptest.NewRequest(
			http.MethodGet,
			"/v1/models",
			nil,
		),
	)
	if public.Code != http.StatusUnauthorized ||
		!strings.Contains(
			public.Body.String(),
			`"code":"unauthorized"`,
		) ||
		!strings.Contains(
			public.Body.String(),
			`"request_id":"llmreq_`,
		) ||
		!strings.HasPrefix(
			public.Header().Get(
				"X-Local-Request-ID",
			),
			"llmreq_",
		) {
		t.Fatalf(
			"public status = %d, headers = %v, body = %s",
			public.Code,
			public.Header(),
			public.Body.String(),
		)
	}

	provisioning := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		provisioning,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-keys/provision", nil),
	)
	if provisioning.Code != http.StatusUnauthorized ||
		!strings.Contains(provisioning.Body.String(), `"code":"provisioning_unauthorized"`) ||
		!strings.Contains(provisioning.Body.String(), `"request_id":"provreq_`) {
		t.Fatalf(
			"provisioning status = %d, body = %s",
			provisioning.Code,
			provisioning.Body.String(),
		)
	}

	admin := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		admin,
		httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil),
	)
	if admin.Code != http.StatusUnauthorized ||
		!strings.Contains(admin.Body.String(), `"code":"admin_unauthorized"`) {
		t.Fatalf("admin status = %d, body = %s", admin.Code, admin.Body.String())
	}
}

func TestNewTransportGraphLeavesProvisioningUnregisteredWhenDisabled(t *testing.T) {
	cfg, _, _, _, billingInfrastructure, forwardingInfrastructure, repositories := validApplicationGraphInputs(t)
	cfg.ProvisioningServiceToken = ""
	cfg.APIKeyProvisioningEncryptionKey = nil

	security, err := NewSecurityGraph(cfg)
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	provisioningInfrastructure, err := NewProvisioningInfrastructureGraph(cfg, security)
	if err != nil {
		t.Fatalf("NewProvisioningInfrastructureGraph: %v", err)
	}

	graph := buildTransportGraph(
		t,
		cfg,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)
	if graph.ProvisioningEnabled || graph.Provisioning != nil {
		t.Fatal("disabled provisioning handler was registered")
	}

	response := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-keys/provision", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestNewTransportGraphRejectsCapabilityMismatch(t *testing.T) {
	cfg, primitives, security, provisioningInfrastructure, billingInfrastructure, forwardingInfrastructure, repositories :=
		validApplicationGraphInputs(t)
	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		TelegramInfrastructureGraph{}, validLoggingGraph(t),

		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}

	security.ProvisioningEnabled = false
	security.ProvisioningAuthenticator = nil
	if err := security.Validate(); err != nil {
		t.Fatalf("mutated security graph: %v", err)
	}

	graph, err := NewTransportGraph(cfg, primitives, security, applications)
	if err == nil {
		t.Fatal("expected provisioning capability mismatch error")
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("invalid transport graph unexpectedly validated")
	}
}

func TestTransportGraphValidateRejectsMissingCapability(t *testing.T) {
	var graph TransportGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}
