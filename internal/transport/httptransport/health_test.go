package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, HealthPath, nil)

	HealthHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q", got)
	}
	if response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("body=%q", response.Body.String())
	}
}

func TestHealthHandlerRejectsNonGET(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, HealthPath, nil)

	HealthHandler(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow=%q", got)
	}
}
