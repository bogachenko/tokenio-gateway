package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewTransportGraphWiresAdminControlPlane(t *testing.T) {
	cfg, _, security, billingInfrastructure, repositories :=
		validApplicationGraphInputs(t)

	primitives, err := NewRuntimePrimitives()
	if err != nil {
		t.Fatalf("NewRuntimePrimitives: %v", err)
	}
	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		billingInfrastructure,
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}

	graph, err := NewTransportGraph(
		primitives,
		security,
		applications,
	)
	if err != nil {
		t.Fatalf("NewTransportGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("transport graph: %v", err)
	}

	health := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		health,
		httptest.NewRequest(http.MethodGet, "/health", nil),
	)
	if health.Code != http.StatusOK || health.Body.String() != "OK" {
		t.Fatalf(
			"health status = %d, body = %q",
			health.Code,
			health.Body.String(),
		)
	}

	admin := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		admin,
		httptest.NewRequest(
			http.MethodGet,
			"/admin/v1/users",
			nil,
		),
	)
	if admin.Code != http.StatusUnauthorized {
		t.Fatalf(
			"admin status = %d, body = %s",
			admin.Code,
			admin.Body.String(),
		)
	}
	requestID := admin.Header().Get("X-Admin-Request-ID")
	if !strings.HasPrefix(requestID, "admreq_") {
		t.Fatalf("admin request ID = %q", requestID)
	}
	if !strings.Contains(
		admin.Body.String(),
		`"code":"admin_unauthorized"`,
	) {
		t.Fatalf("admin body = %s", admin.Body.String())
	}

	public := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		public,
		httptest.NewRequest(
			http.MethodGet,
			"/v1/users",
			nil,
		),
	)
	if public.Code != http.StatusNotFound {
		t.Fatalf(
			"public status = %d, body = %s",
			public.Code,
			public.Body.String(),
		)
	}
}

func TestTransportGraphValidateRejectsMissingCapability(t *testing.T) {
	var graph TransportGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}
