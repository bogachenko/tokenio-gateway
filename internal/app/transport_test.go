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
	cfg, _, security, provisioningInfrastructure, billingInfrastructure, repositories :=
		validApplicationGraphInputs(t)

	graph := buildTransportGraph(
		t,
		cfg,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		repositories,
	)
	if err := graph.Validate(); err != nil {
		t.Fatalf("transport graph: %v", err)
	}
	if !graph.ProvisioningEnabled || graph.Provisioning == nil {
		t.Fatal("provisioning HTTP handler is not wired")
	}

	provisioning := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		provisioning,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-key-provisionings", nil),
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
	cfg, _, _, _, billingInfrastructure, repositories := validApplicationGraphInputs(t)
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
		repositories,
	)
	if graph.ProvisioningEnabled || graph.Provisioning != nil {
		t.Fatal("disabled provisioning handler was registered")
	}

	response := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-key-provisionings", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestNewTransportGraphRejectsCapabilityMismatch(t *testing.T) {
	cfg, primitives, security, provisioningInfrastructure, billingInfrastructure, repositories :=
		validApplicationGraphInputs(t)
	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
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
