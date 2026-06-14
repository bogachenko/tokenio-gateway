package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			if response.Header().Get("Connection") != "" ||
				response.Header().Get("X-Billing-Amount-Cents") != "" {
				t.Fatalf("unsafe upstream headers leaked: %#v", response.Header())
			}
			if response.Header().Get("X-Local-Request-ID") != "llmreq_transport_1" ||
				response.Header().Get("X-Billing-Provider-Type") != "openai" ||
				response.Header().Get("X-Billing-Client-Model") != "client-model" ||
				response.Header().Get("X-Billing-Model") != "openai:client-model" ||
				response.Header().Get("X-Billing-Currency") != "RUB" {
				t.Fatalf("gateway headers=%#v", response.Header())
			}
		})
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
