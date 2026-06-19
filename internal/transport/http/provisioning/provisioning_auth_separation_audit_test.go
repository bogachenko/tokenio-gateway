package provisioninghttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestProvisioningAuthIsSeparateFromPublicAndAdminAuth(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*http.Request)
		wantCalls int
	}{
		{
			name: "public bearer api key is ignored",
			mutate: func(request *http.Request) {
				request.Header.Del(serviceTokenHeader)
				request.Header.Set(
					"Authorization",
					"Bearer sk_live_public_api_key",
				)
			},
		},
		{
			name: "admin bearer token is ignored",
			mutate: func(request *http.Request) {
				request.Header.Del(serviceTokenHeader)
				request.Header.Set(
					"Authorization",
					"Bearer admin-secret",
				)
			},
		},
		{
			name: "configured service token is accepted without authorization header",
			mutate: func(request *http.Request) {
				request.Header.Set(serviceTokenHeader, "service-token")
				request.Header.Del("Authorization")
			},
			wantCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &routerService{}
			request := validProvisionRequest()
			test.mutate(request)
			response := httptest.NewRecorder()

			newTestRouter(t, service).ServeHTTP(response, request)

			if test.wantCalls == 0 {
				assertError(
					t,
					response,
					http.StatusUnauthorized,
					domain.ErrorCodeProvisioningUnauthorized,
				)
				if service.provisionCalls != 0 {
					t.Fatalf(
						"provisioning service called without service token: %d",
						service.provisionCalls,
					)
				}
				if strings.Contains(response.Body.String(), "sk_live_public_api_key") ||
					strings.Contains(response.Body.String(), "admin-secret") {
					t.Fatalf("auth secret leaked: %s", response.Body.String())
				}
				return
			}

			if response.Code != http.StatusOK {
				t.Fatalf(
					"status=%d body=%s",
					response.Code,
					response.Body.String(),
				)
			}
			if service.provisionCalls != test.wantCalls {
				t.Fatalf(
					"provision calls=%d, want %d",
					service.provisionCalls,
					test.wantCalls,
				)
			}
		})
	}
}
