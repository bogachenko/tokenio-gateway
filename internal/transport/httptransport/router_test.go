package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type rootRouterHandler struct {
	calls int
}

func (h *rootRouterHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(http.StatusNoContent)
}

func TestNewRouterContract(t *testing.T) {
	router, err := NewRouter(nil, nil)
	if router != nil || !errors.Is(err, ErrInvalidRouterConfig) {
		t.Fatalf("router = %v, error = %v", router, err)
	}

	router, err = NewRouter(&rootRouterHandler{}, nil)
	if err != nil || router == nil {
		t.Fatalf("disabled provisioning router: %v", err)
	}
}

func TestRouterHealthAndErrors(t *testing.T) {
	tests := []struct {
		method      string
		path        string
		wantStatus  int
		wantType    string
		wantBody    string
		wantCode    domain.ErrorCode
		wantMessage string
		wantAllow   string
	}{
		{
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
			wantType:   "text/plain",
			wantBody:   "OK",
		},
		{
			method:      http.MethodPost,
			path:        "/health",
			wantStatus:  http.StatusMethodNotAllowed,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeMethodNotAllowed,
			wantMessage: "Method not allowed",
			wantAllow:   http.MethodGet,
		},
		{
			method:      http.MethodGet,
			path:        "/v1/models",
			wantStatus:  http.StatusNotFound,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeNotFound,
			wantMessage: "Not found",
		},
	}

	router, err := NewRouter(&rootRouterHandler{}, nil)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != test.wantStatus {
			t.Fatalf("path %s: status = %d, want %d", test.path, recorder.Code, test.wantStatus)
		}
		if recorder.Header().Get("Content-Type") != test.wantType {
			t.Fatalf("path %s: Content-Type = %q", test.path, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("Allow") != test.wantAllow {
			t.Fatalf("path %s: Allow = %q", test.path, recorder.Header().Get("Allow"))
		}
		if test.wantBody != "" {
			if recorder.Body.String() != test.wantBody {
				t.Fatalf("path %s: body = %q", test.path, recorder.Body.String())
			}
			continue
		}
		var response ErrorResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if response.Error.Code != test.wantCode || response.Error.Message != test.wantMessage {
			t.Fatalf("path %s: error = %#v", test.path, response.Error)
		}
	}
}

func TestRouterDispatchesOnlyExactBoundaries(t *testing.T) {
	tests := []struct {
		path             string
		wantStatus       int
		wantAdminCalls   int
		wantProvisioning int
	}{
		{path: "/admin/v1", wantStatus: http.StatusNoContent, wantAdminCalls: 1},
		{path: "/admin/v1/users", wantStatus: http.StatusNoContent, wantAdminCalls: 1},
		{path: "/admin/v1evil", wantStatus: http.StatusNotFound},
		{path: "/internal/v1/api-key-provisionings", wantStatus: http.StatusNoContent, wantProvisioning: 1},
		{path: "/internal/v1/api-key-provisionings/prov_1/confirm-delivery", wantStatus: http.StatusNoContent, wantProvisioning: 1},
		{path: "/internal/v1/api-key-provisioningsevil", wantStatus: http.StatusNotFound},
		{path: "/internal/v1", wantStatus: http.StatusNotFound},
		{path: "/v1/users", wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		admin := &rootRouterHandler{}
		provisioning := &rootRouterHandler{}
		router, err := NewRouter(admin, provisioning)
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.wantStatus {
			t.Fatalf("path %s: status = %d, want %d", test.path, recorder.Code, test.wantStatus)
		}
		if admin.calls != test.wantAdminCalls || provisioning.calls != test.wantProvisioning {
			t.Fatalf(
				"path %s: admin calls = %d, provisioning calls = %d",
				test.path,
				admin.calls,
				provisioning.calls,
			)
		}
	}
}

func TestRouterDoesNotDispatchDisabledProvisioning(t *testing.T) {
	router, err := NewRouter(&rootRouterHandler{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-key-provisionings", nil),
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
