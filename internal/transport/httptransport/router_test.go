package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

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
		{name: "health ok", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK, wantType: "text/plain", wantBody: "OK"},
		{name: "health wrong method", method: http.MethodPost, path: "/health", wantStatus: http.StatusMethodNotAllowed, wantType: "application/json", wantCode: domain.ErrorCodeMethodNotAllowed, wantMessage: "Method not allowed", wantAllow: http.MethodGet},
		{name: "health slash not found", method: http.MethodGet, path: "/health/", wantStatus: http.StatusNotFound, wantType: "application/json", wantCode: domain.ErrorCodeNotFound, wantMessage: "Not found"},
		{name: "unknown not found", method: http.MethodGet, path: "/v1/models", wantStatus: http.StatusNotFound, wantType: "application/json", wantCode: domain.ErrorCodeNotFound, wantMessage: "Not found"},
	}

	router := NewRouter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if got := recorder.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if tt.wantBody != "" {
				if got := recorder.Body.String(); got != tt.wantBody {
					t.Fatalf("body = %q, want %q", got, tt.wantBody)
				}
				return
			}

			var response ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error.Code != tt.wantCode || response.Error.Message != tt.wantMessage {
				t.Fatalf("error = %#v, want code %q message %q", response.Error, tt.wantCode, tt.wantMessage)
			}
			if response.Error.RequestID != "" {
				t.Fatalf("request_id = %q, want omitted", response.Error.RequestID)
			}
		})
	}
}
