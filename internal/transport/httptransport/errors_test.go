package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestWriteGatewayErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteGatewayError(recorder, GatewayError{Status: http.StatusBadRequest, Code: domain.ErrorCodeInvalidJSON, Message: "Invalid JSON", RequestID: "llmreq_1"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != domain.ErrorCodeInvalidJSON || response.Error.Message != "Invalid JSON" || response.Error.RequestID != "llmreq_1" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestUnexpectedErrorEnvelopeDoesNotSerializeRawError(t *testing.T) {
	recorder := httptest.NewRecorder()
	secretText := "database password secret"
	_ = secretText
	WriteGatewayError(recorder, internalGatewayError("llmreq_2"))

	body := recorder.Body.String()
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(body, secretText) {
		t.Fatalf("body leaked secret raw error: %s", body)
	}
	if !strings.Contains(body, "Internal server error") || !strings.Contains(body, string(domain.ErrorCodeInternalError)) {
		t.Fatalf("body did not contain normalized internal error: %s", body)
	}
}
