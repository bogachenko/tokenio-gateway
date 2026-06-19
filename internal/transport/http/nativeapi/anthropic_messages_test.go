package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type anthropicAuthFake struct {
	result authenticateapp.Result
	err    error
	calls  int
	rawKey string
}

func (f *anthropicAuthFake) AuthenticatePublicRequest(
	_ context.Context,
	input authenticateapp.Input,
) (authenticateapp.Result, error) {
	f.calls++
	f.rawKey = input.RawAPIKey
	return f.result, f.err
}

type anthropicMessagesFake struct {
	result AnthropicMessagesResult
	err    error
	calls  int
	input  AnthropicMessagesInput
}

func (f *anthropicMessagesFake) HandleAnthropicMessages(
	_ context.Context,
	input AnthropicMessagesInput,
) (AnthropicMessagesResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

type anthropicRequestIDsFake struct {
	local string
	err   error
	calls int
}

func (f *anthropicRequestIDsFake) NewLocalRequestID() (string, error) {
	f.calls++
	return f.local, f.err
}

func (*anthropicRequestIDsFake) NewAdminRequestID() (string, error) {
	return "", errors.New("unexpected admin request id")
}

func (*anthropicRequestIDsFake) NewProvisioningRequestID() (string, error) {
	return "", errors.New("unexpected provisioning request id")
}

func TestAnthropicMessagesEndpointAuthenticatesExtractsModelAndPreservesBody(
	t *testing.T,
) {
	authentication := successfulAnthropicAuthentication()
	messages := &anthropicMessagesFake{
		result: AnthropicMessagesResult{
			Status: http.StatusCreated,
			Header: http.Header{"Content-Type": {"application/json"}},
			RawBody: []byte(
				`{"id":"msg_1","type":"message","content":[]}`,
			),
		},
	}
	router := mustAnthropicRouter(t, authentication, messages)

	body := `{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hi"}]}`
	request := httptest.NewRequest(
		http.MethodPost,
		anthropicMessagesPath,
		strings.NewReader(body),
	)
	request.Header.Set("x-api-key", "sk_live_anthropic_test")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Local-Request-ID") != "llmreq_native_1" {
		t.Fatalf(
			"request id header = %q",
			response.Header().Get("X-Local-Request-ID"),
		)
	}
	if authentication.calls != 1 ||
		authentication.rawKey != "sk_live_anthropic_test" ||
		messages.calls != 1 {
		t.Fatalf(
			"auth calls=%d raw=%q messages calls=%d",
			authentication.calls,
			authentication.rawKey,
			messages.calls,
		)
	}
	if messages.input.Model != "claude-3-5-sonnet-latest" ||
		string(messages.input.RawBody) != body ||
		messages.input.Authentication.Principal.UserID != "usr_native_1" {
		t.Fatalf("input = %#v", messages.input)
	}
	if strings.Contains(response.Body.String(), "sk_live_anthropic_test") {
		t.Fatal("response leaked inbound Tokenio API key")
	}
}

func TestAnthropicMessagesRejectsWrongCarrierAndQueryKey(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		target  string
		message string
	}{
		{
			name: "authorization bearer",
			headers: map[string]string{
				"Authorization": "Bearer sk_live_wrong",
			},
			target:  anthropicMessagesPath,
			message: "credential carrier conflicts with API family",
		},
		{
			name: "gemini carrier",
			headers: map[string]string{
				"x-goog-api-key": "sk_live_wrong",
			},
			target:  anthropicMessagesPath,
			message: "credential carrier conflicts with API family",
		},
		{
			name: "query key",
			headers: map[string]string{
				"x-api-key": "sk_live_test",
			},
			target:  anthropicMessagesPath + "?key=sk_in_url",
			message: "query-string API keys are not allowed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication := successfulAnthropicAuthentication()
			messages := &anthropicMessagesFake{}
			router := mustAnthropicRouter(t, authentication, messages)

			request := httptest.NewRequest(
				http.MethodPost,
				test.target,
				strings.NewReader(`{"model":"claude","messages":[]}`),
			)
			for key, value := range test.headers {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertAnthropicError(
				t,
				response,
				http.StatusUnauthorized,
				domain.ErrorCodeUnauthorized,
				test.message,
			)
			if authentication.calls != 0 || messages.calls != 0 {
				t.Fatalf(
					"auth calls=%d messages calls=%d",
					authentication.calls,
					messages.calls,
				)
			}
		})
	}
}

func TestAnthropicMessagesRejectsMissingModelBeforeHandler(t *testing.T) {
	authentication := successfulAnthropicAuthentication()
	messages := &anthropicMessagesFake{}
	router := mustAnthropicRouter(t, authentication, messages)

	request := httptest.NewRequest(
		http.MethodPost,
		anthropicMessagesPath,
		strings.NewReader(`{"messages":[]}`),
	)
	request.Header.Set("x-api-key", "sk_live_test")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertAnthropicError(
		t,
		response,
		http.StatusBadRequest,
		domain.ErrorCodeModelRequired,
		"model is required",
	)
	if authentication.calls != 1 || messages.calls != 0 {
		t.Fatalf(
			"auth calls=%d messages calls=%d",
			authentication.calls,
			messages.calls,
		)
	}
}

func TestAnthropicMessagesRejectsUnknownPath(t *testing.T) {
	router := mustAnthropicRouter(
		t,
		successfulAnthropicAuthentication(),
		&anthropicMessagesFake{},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude","messages":[]}`),
	)
	request.Header.Set("x-api-key", "sk_live_test")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertAnthropicError(
		t,
		response,
		http.StatusNotFound,
		domain.ErrorCodeNotFound,
		"Endpoint not found",
	)
}

func successfulAnthropicAuthentication() *anthropicAuthFake {
	return &anthropicAuthFake{
		result: authenticateapp.Result{
			Principal: auth.APIKeyPrincipal{
				UserID:               "usr_native_1",
				APIKeyID:             "key_native_1",
				BillingSubjectUserID: "billing_native_1",
			},
		},
	}
}

func mustAnthropicRouter(
	t *testing.T,
	authentication *anthropicAuthFake,
	messages *anthropicMessagesFake,
) *AnthropicMessagesRouter {
	t.Helper()
	router, err := NewAnthropicMessagesRouter(
		authentication,
		messages,
		&anthropicRequestIDsFake{local: "llmreq_native_1"},
	)
	if err != nil {
		t.Fatalf("NewAnthropicMessagesRouter: %v", err)
	}
	return router
}

func assertAnthropicError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code domain.ErrorCode,
	message string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var payload nativeErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != code ||
		payload.Error.Message != message ||
		payload.Error.RequestID != "llmreq_native_1" {
		t.Fatalf("error payload = %#v", payload)
	}
}
