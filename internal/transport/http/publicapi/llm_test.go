package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	llmrequestapp "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type testLLMRequests struct {
	result llmrequestapp.ForwardedRequest
	err    error
	calls  int
	input  llmrequestapp.Input
}

func (r *testLLMRequests) Execute(
	_ context.Context,
	input llmrequestapp.Input,
) (llmrequestapp.ForwardedRequest, error) {
	r.calls++
	r.input = input
	return r.result, r.err
}

func successfulLLMResult(
	requestID string,
	endpoint domain.EndpointKind,
	body []byte,
) llmrequestapp.ForwardedRequest {
	return llmrequestapp.ForwardedRequest{
		Reserved: llmrequestapp.ReservedRequest{
			Prepared: llmrequestapp.PreparedRequest{
				LocalRequestID: requestID,
				APIFamily:      domain.APIFamilyOpenAICompatible,
				EndpointKind:   endpoint,
				ClientModel:    "client-model",
				Plan: llmrequestapp.RoutePlan{
					Route: domain.Route{
						ProviderType: domain.ProviderOpenAI,
					},
					BillingModel: "openai:client-model",
					Currency:     "RUB",
				},
			},
			Admission: llmrequestapp.BillingAdmissionResult{
				RemoteBalanceCents:    1000,
				PendingAmountCents:    100,
				EffectiveBalanceCents: 900,
				Currency:              "RUB",
			},
		},
		FinalUsageRecord: domain.UsageRecord{
			LocalRequestID:       requestID,
			ProviderType:         domain.ProviderOpenAI,
			ClientModel:          "client-model",
			BillingModel:         "openai:client-model",
			Currency:             "RUB",
			ClientAmountCents:    15,
			RemainingAmountCents: 15,
			UsageCompleteness:    "detailed",
			Status:               domain.UsageStatusBillable,
			Usage: domain.TokenUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		},
		AutoCharge: llmrequestapp.AutoChargeResult{
			Status: llmrequestapp.AutoChargeStatusDeferred,
		},
		Response: ports.ForwardResponse{
			StatusCode: http.StatusCreated,
			Headers: map[string][]string{
				"Content-Type":           {"application/json"},
				"Connection":             {"close"},
				"X-Billing-Amount-Cents": {"999999"},
			},
			Body: body,
		},
	}
}

func TestLLMRouterDispatchesNormalizedInputAndPassesResponseThrough(
	t *testing.T,
) {
	tests := []struct {
		path string
		kind domain.EndpointKind
	}{
		{chatCompletionsPath, domain.EndpointChat},
		{embeddingsPath, domain.EndpointEmbeddings},
		{imageGenerationsPath, domain.EndpointImagesGeneration},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			body := []byte(`{"model":"client-model","opaque":{"x":1}}`)
			requests := &testLLMRequests{
				result: successfulLLMResult(
					"llmreq_transport_1",
					test.kind,
					[]byte(`{"upstream":true}`),
				),
			}
			router, err := NewLLMRouter(
				requests,
				&testRequestIDs{local: "llmreq_transport_1"},
				1024,
			)
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(
				http.MethodPost,
				test.path,
				strings.NewReader(string(body)),
			)
			request.Header.Set("Authorization", "Bearer sk_live_test")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "idem-1")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Body.String() != `{"upstream":true}` {
				t.Fatalf("body=%q", response.Body.String())
			}
			if requests.calls != 1 {
				t.Fatalf("calls=%d", requests.calls)
			}
			if requests.input.LocalRequestID != "llmreq_transport_1" ||
				requests.input.RawAPIKey != "sk_live_test" ||
				requests.input.APIFamily != domain.APIFamilyOpenAICompatible ||
				requests.input.EndpointKind != test.kind ||
				string(requests.input.Payload) != string(body) ||
				requests.input.IdempotencyKey == nil ||
				*requests.input.IdempotencyKey != "idem-1" {
				t.Fatalf("input=%+v", requests.input)
			}
			if response.Header().Get("Connection") != "" {
				t.Fatalf("unsafe upstream headers leaked: %#v", response.Header())
			}
			if response.Header().Get("X-Local-Request-ID") != "llmreq_transport_1" ||
				response.Header().Get("X-Billing-Provider-Type") != "openai" ||
				response.Header().Get("X-Billing-Client-Model") != "client-model" ||
				response.Header().Get("X-Billing-Model") != "openai:client-model" ||
				response.Header().Get("X-Billing-Currency") != "RUB" ||
				response.Header().Get("X-Billing-Amount-Cents") != "15" ||
				response.Header().Get("X-Billing-Remaining-Cents") != "15" ||
				response.Header().Get("X-Billing-Input-Tokens") != "10" ||
				response.Header().Get("X-Billing-Cached-Input-Tokens") != "0" ||
				response.Header().Get("X-Billing-Output-Tokens") != "5" ||
				response.Header().Get("X-Billing-Reasoning-Tokens") != "0" ||
				response.Header().Get("X-Billing-Image-Input-Tokens") != "0" ||
				response.Header().Get("X-Billing-Audio-Input-Tokens") != "0" ||
				response.Header().Get("X-Billing-Audio-Output-Tokens") != "0" ||
				response.Header().Get("X-Billing-File-Input-Tokens") != "0" ||
				response.Header().Get("X-Billing-Video-Input-Tokens") != "0" ||
				response.Header().Get("X-Billing-Usage-Completeness") != "detailed" ||
				response.Header().Get("X-Billing-Status") != "billable" ||
				response.Header().Get("X-Billing-Auto-Charge-Status") != "deferred" ||
				response.Header().Get("X-Wallet-Balance-Cents") != "1000" ||
				response.Header().Get("X-Wallet-Effective-Balance-Cents") != "900" ||
				response.Header().Get("X-Billing-Pending-Cents") != "100" ||
				response.Header().Get("X-Auto-Charge-Status") != "" ||
				response.Header().Get("X-Auto-Charge-Charged-Cents") != "" {
				t.Fatalf("gateway headers=%#v", response.Header())
			}
		})
	}
}

func TestLLMRouterAcceptsMissingContentTypeAsJSON(t *testing.T) {
	const body = `{"model":"client-model","opaque":{"x":1}}`
	requests := &testLLMRequests{
		result: successfulLLMResult(
			"llmreq_missing_content_type",
			domain.EndpointChat,
			[]byte(`{"ok":true}`),
		),
	}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_missing_content_type"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		chatCompletionsPath,
		strings.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer sk_live_test")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if requests.calls != 1 || string(requests.input.Payload) != body {
		t.Fatalf("calls=%d input=%+v", requests.calls, requests.input)
	}
}

func TestLLMRouterRejectsMethodBeforeApplicationCall(t *testing.T) {
	requests := &testLLMRequests{}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_method_1"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, chatCompletionsPath, nil),
	)
	if response.Code != http.StatusMethodNotAllowed ||
		response.Header().Get("Allow") != http.MethodPost ||
		requests.calls != 0 {
		t.Fatalf(
			"status=%d allow=%q calls=%d body=%s",
			response.Code,
			response.Header().Get("Allow"),
			requests.calls,
			response.Body.String(),
		)
	}
}

func TestLLMRouterRejectsInvalidBodyBeforeApplicationCall(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64
		status      int
	}{
		{
			name:        "invalid json",
			contentType: "application/json",
			body:        "{",
			limit:       1024,
			status:      http.StatusBadRequest,
		},
		{
			name:        "unsupported content type",
			contentType: "text/plain",
			body:        "{}",
			limit:       1024,
			status:      http.StatusUnsupportedMediaType,
		},
		{
			name:        "too large",
			contentType: "application/json",
			body:        `{"model":"client-model"}`,
			limit:       2,
			status:      http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := &testLLMRequests{}
			router, err := NewLLMRouter(
				requests,
				&testRequestIDs{local: "llmreq_body_1"},
				test.limit,
			)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				chatCompletionsPath,
				strings.NewReader(test.body),
			)
			request.Header.Set("Authorization", "Bearer sk_live_test")
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != test.status || requests.calls != 0 {
				t.Fatalf(
					"status=%d want=%d calls=%d body=%s",
					response.Code,
					test.status,
					requests.calls,
					response.Body.String(),
				)
			}
		})
	}
}

func TestLLMRouterMapsApplicationFailureWithoutLeakage(t *testing.T) {
	requests := &testLLMRequests{
		err: errors.New("postgres sk_live_secret"),
	}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_failure_1"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		chatCompletionsPath,
		strings.NewReader(`{"model":"client-model"}`),
	)
	request.Header.Set("Authorization", "Bearer sk_live_test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError ||
		strings.Contains(response.Body.String(), "sk_live_") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLLMRouterMapsApplicationErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    domain.ErrorCode
		message string
	}{
		{"invalid api key", authenticateapp.ErrInvalidAPIKey, http.StatusUnauthorized, domain.ErrorCodeInvalidAPIKey, "Invalid API key"},
		{"disabled user", authenticateapp.ErrUserDisabled, http.StatusForbidden, domain.ErrorCodeUserDisabled, "User is disabled"},
		{"invalid JSON", llmrequestapp.ErrInvalidJSON, http.StatusBadRequest, domain.ErrorCodeInvalidJSON, "Request body must contain valid JSON"},
		{"model required", llmrequestapp.ErrModelRequired, http.StatusBadRequest, domain.ErrorCodeModelRequired, "Model is required"},
		{"streaming unsupported", llmrequestapp.ErrStreamingUnsupported, http.StatusBadRequest, domain.ErrorCodeStreamingUnsupported, "Streaming is not supported"},
		{"unknown model", llmrequestapp.ErrUnknownModel, http.StatusBadRequest, domain.ErrorCodeUnknownModel, "Unknown model"},
		{"unsupported capability", llmrequestapp.ErrUnsupportedCapability, http.StatusBadRequest, domain.ErrorCodeUnsupportedCapability, "Unsupported capability"},
		{"no route", llmrequestapp.ErrNoRouteAvailable, http.StatusServiceUnavailable, domain.ErrorCodeNoRouteAvailable, "No route is available"},
		{"pricing unavailable", llmrequestapp.ErrPricingUnavailable, http.StatusServiceUnavailable, domain.ErrorCodePricingUnavailable, "Pricing is unavailable"},
		{"billing insufficient funds", billingapp.ErrInsufficientFunds, http.StatusPaymentRequired, domain.ErrorCodeInsufficientFunds, "Insufficient balance"},
		{"LLM idempotency key reused", llmrequestapp.ErrIdempotencyKeyReused, http.StatusConflict, domain.ErrorCodeIdempotencyKeyReused, "Idempotency key conflicts with an existing request"},
		{"ledger idempotency key reused", ledgerapp.ErrIdempotencyKeyReused, http.StatusConflict, domain.ErrorCodeIdempotencyKeyReused, "Idempotency key conflicts with an existing request"},
		{"LLM request in progress", llmrequestapp.ErrRequestInProgress, http.StatusConflict, domain.ErrorCodeRequestInProgress, "Request is already in progress"},
		{"ledger request in progress", ledgerapp.ErrRequestInProgress, http.StatusConflict, domain.ErrorCodeRequestInProgress, "Request is already in progress"},
		{"LLM replay unavailable", llmrequestapp.ErrIdempotencyReplayNotAvailable, http.StatusConflict, domain.ErrorCodeIdempotencyReplayNotAvailable, "Idempotency replay is not available"},
		{"ledger replay unavailable", ledgerapp.ErrIdempotencyReplayNotAvailable, http.StatusConflict, domain.ErrorCodeIdempotencyReplayNotAvailable, "Idempotency replay is not available"},
		{"LLM unresolved usage", llmrequestapp.ErrUnresolvedUsage, http.StatusConflict, domain.ErrorCodeUnresolvedUsage, "Previous usage requires resolution"},
		{"billing identity unavailable", billingapp.ErrBillingIdentityUnavailable, http.StatusBadGateway, domain.ErrorCodeBillingUnavailable, "Billing service is unavailable"},
		{"billing unavailable", billingapp.ErrBillingUnavailable, http.StatusBadGateway, domain.ErrorCodeBillingUnavailable, "Billing service is unavailable"},
		{"billing usage store unavailable", billingapp.ErrBillingStoreUnavailable, http.StatusServiceUnavailable, domain.ErrorCodeUsageStoreUnavailable, "Usage store is unavailable"},
		{"raw deadline", context.DeadlineExceeded, http.StatusInternalServerError, domain.ErrorCodeInternalError, "Internal error"},
		{"internal", errors.New("postgres sk_live_secret"), http.StatusInternalServerError, domain.ErrorCodeInternalError, "Internal error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := &testLLMRequests{err: test.err}
			router, err := NewLLMRouter(
				requests,
				&testRequestIDs{local: "llmreq_mapping_1"},
				1024,
			)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(
				http.MethodPost,
				chatCompletionsPath,
				strings.NewReader(`{"model":"client-model"}`),
			)
			request.Header.Set("Authorization", "Bearer sk_live_test")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertError(
				t,
				response,
				test.status,
				test.code,
				test.message,
				"llmreq_mapping_1",
			)
			if strings.Contains(response.Body.String(), "sk_live_") ||
				strings.Contains(response.Body.String(), "postgres") {
				t.Fatalf("internal detail leaked: %s", response.Body.String())
			}
		})
	}
}

func TestLLMRouterPassesThroughPricingFailedSuccess(t *testing.T) {
	body := []byte(`{"upstream":"unchanged"}`)
	result := successfulLLMResult(
		"llmreq_pricing_failed_transport",
		domain.EndpointChat,
		body,
	)
	result.Response.StatusCode = http.StatusAccepted
	result.FinalUsageRecord.Status = domain.UsageStatusPricingFailed
	result.FinalUsageRecord.ClientAmountCents = 0
	result.FinalUsageRecord.RemainingAmountCents = 0
	result.FinalUsageRecord.UsageCompleteness = "failed"
	result.AutoCharge = llmrequestapp.AutoChargeResult{}

	requests := &testLLMRequests{result: result}
	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: "llmreq_pricing_failed_transport"},
		1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		chatCompletionsPath,
		strings.NewReader(`{"model":"client-model"}`),
	)
	request.Header.Set("Authorization", "Bearer sk_live_test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.String() != string(body) {
		t.Fatalf("body=%q", response.Body.String())
	}
	if response.Header().Get("X-Billing-Status") != "pricing_failed" {
		t.Fatalf("headers=%#v", response.Header())
	}
}
