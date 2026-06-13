package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type rootRouterAdminHandler struct {
	calls int
}

func (h *rootRouterAdminHandler) ServeHTTP(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	h.calls++
	writer.WriteHeader(http.StatusNoContent)
}

func TestNewRouterRejectsNilAdminHandler(t *testing.T) {
	router, err := NewRouter(nil)
	if router != nil {
		t.Fatal("router must be nil")
	}
	if !errors.Is(err, ErrInvalidRouterConfig) {
		t.Fatalf("error = %v, want ErrInvalidRouterConfig", err)
	}
}

func TestRouterHealthAndErrors(t *testing.T) {
	tests := []struct {
		name        string
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
			name:       "health ok",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
			wantType:   "text/plain",
			wantBody:   "OK",
		},
		{
			name:        "health wrong method",
			method:      http.MethodPost,
			path:        "/health",
			wantStatus:  http.StatusMethodNotAllowed,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeMethodNotAllowed,
			wantMessage: "Method not allowed",
			wantAllow:   http.MethodGet,
		},
		{
			name:        "health slash not found",
			method:      http.MethodGet,
			path:        "/health/",
			wantStatus:  http.StatusNotFound,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeNotFound,
			wantMessage: "Not found",
		},
		{
			name:        "unknown not found",
			method:      http.MethodGet,
			path:        "/v1/models",
			wantStatus:  http.StatusNotFound,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeNotFound,
			wantMessage: "Not found",
		},
	}

	router, err := NewRouter(&rootRouterAdminHandler{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(
				recorder,
				httptest.NewRequest(
					test.method,
					test.path,
					nil,
				),
			)

			if recorder.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					recorder.Code,
					test.wantStatus,
				)
			}
			if got := recorder.Header().Get("Content-Type"); got != test.wantType {
				t.Fatalf(
					"Content-Type = %q, want %q",
					got,
					test.wantType,
				)
			}
			if got := recorder.Header().Get("Allow"); got != test.wantAllow {
				t.Fatalf(
					"Allow = %q, want %q",
					got,
					test.wantAllow,
				)
			}
			if test.wantBody != "" {
				if got := recorder.Body.String(); got != test.wantBody {
					t.Fatalf(
						"body = %q, want %q",
						got,
						test.wantBody,
					)
				}
				return
			}

			var response ErrorResponse
			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error.Code != test.wantCode ||
				response.Error.Message != test.wantMessage {
				t.Fatalf(
					"error = %#v, want code %q message %q",
					response.Error,
					test.wantCode,
					test.wantMessage,
				)
			}
			if response.Error.RequestID != "" {
				t.Fatalf(
					"request_id = %q, want omitted",
					response.Error.RequestID,
				)
			}
		})
	}
}

func TestRouterDispatchesOnlyExactAdminBoundary(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantDispatch bool
	}{
		{
			name:         "admin root",
			path:         "/admin/v1",
			wantStatus:   http.StatusNoContent,
			wantDispatch: true,
		},
		{
			name:         "admin endpoint",
			path:         "/admin/v1/users",
			wantStatus:   http.StatusNoContent,
			wantDispatch: true,
		},
		{
			name:       "prefix collision",
			path:       "/admin/v1evil",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "admin parent",
			path:       "/admin",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "public path",
			path:       "/v1/users",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			admin := &rootRouterAdminHandler{}
			router, err := NewRouter(admin)
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(
				recorder,
				httptest.NewRequest(
					http.MethodGet,
					test.path,
					nil,
				),
			)

			if recorder.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					recorder.Code,
					test.wantStatus,
				)
			}
			wantCalls := 0
			if test.wantDispatch {
				wantCalls = 1
			}
			if admin.calls != wantCalls {
				t.Fatalf(
					"admin calls = %d, want %d",
					admin.calls,
					wantCalls,
				)
			}
		})
	}
}
