package adminhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
)

func TestAdminAuthIsSeparateFromPublicAPIKeyAuth(t *testing.T) {
	adminAuth, err := auth.NewAdminAuthenticator("admin-secret")
	if err != nil {
		t.Fatal(err)
	}

	service := &testService{}
	router, err := NewRouter(
		service,
		adminAuth,
		&testIDs{value: "admreq_separate_auth"},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/users",
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_live_public_api_key",
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}
	if !strings.Contains(
		response.Body.String(),
		"admin_forbidden",
	) {
		t.Fatalf("body=%s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "sk_live_public_api_key") {
		t.Fatalf(
			"admin auth failure leaked public API key: %s",
			response.Body.String(),
		)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if service.listCalls != 0 {
		t.Fatalf("admin service called with public API key: %d", service.listCalls)
	}
}

func TestAdminAuthAcceptsOnlyConfiguredAdminToken(t *testing.T) {
	adminAuth, err := auth.NewAdminAuthenticator("admin-secret")
	if err != nil {
		t.Fatal(err)
	}

	subject, err := adminAuth.Authenticate("Bearer admin-secret")
	if err != nil {
		t.Fatalf("admin token rejected: %v", err)
	}
	if subject != auth.AdminSubject {
		t.Fatalf("subject=%q", subject)
	}

	if _, err := adminAuth.Authenticate("Bearer sk_live_public_api_key"); err != auth.ErrAdminAccessDenied {
		t.Fatalf("public API key auth error=%v, want ErrAdminAccessDenied", err)
	}
}
