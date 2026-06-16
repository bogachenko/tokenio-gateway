package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmrequestapp "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestLLMRouterEmbeddingsPassesThroughResponseAndFinalBillingHeaders(
	t *testing.T,
) {
	const requestID = "llmreq_embeddings_transport"
	const requestBody = `{
		"model":"embedding-model",
		"input":["ab","c"],
		"encoding_format":"float"
	}`
	const upstreamBody = `{
		"object":"list",
		"data":[{
			"object":"embedding",
			"embedding":[0.1,0.2],
			"index":0
		}],
		"model":"provider-embedding-model",
		"usage":{
			"prompt_tokens":3,
			"total_tokens":3
		}
	}`

	balance := int64(990)
	requests := &testLLMRequests{
		result: llmrequestapp.ForwardedRequest{
			Reserved: llmrequestapp.ReservedRequest{
				Prepared: llmrequestapp.PreparedRequest{
					LocalRequestID: requestID,
					APIFamily: domain.
						APIFamilyOpenAICompatible,
					EndpointKind: domain.EndpointEmbeddings,
					ClientModel:  "embedding-model",
					RequestedCapabilities: domain.
						CapabilitySet{
						Embeddings: true,
					},
					Plan: llmrequestapp.RoutePlan{
						Route: domain.Route{
							ProviderType: domain.ProviderOpenAI,
							EndpointKind: domain.
								EndpointEmbeddings,
						},
						BillingModel: "openai:embedding-model",
						Currency:     "RUB",
					},
				},
				Admission: llmrequestapp.BillingAdmissionResult{
					RemoteBalanceCents:    1000,
					PendingAmountCents:    100,
					EffectiveBalanceCents: 900,
					RequiredReserveCents:  10,
					Currency:              "RUB",
				},
			},
			ResolvedUsage: llmrequestapp.UsageResolutionResult{
				Usage: domain.TokenUsage{
					InputTokens: 3,
				},
				Completeness:          "detailed",
				UpstreamCostCents:     8,
				ClientAmountCents:     10,
				Currency:              "RUB",
				ProviderResponseModel: "provider-embedding-model",
			},
			FinalUsageRecord: domain.UsageRecord{
				LocalRequestID:       requestID,
				ProviderType:         domain.ProviderOpenAI,
				ClientModel:          "embedding-model",
				BillingModel:         "openai:embedding-model",
				Currency:             "RUB",
				ClientAmountCents:    10,
				RemainingAmountCents: 10,
				UsageCompleteness:    "detailed",
				Status:               domain.UsageStatusBillable,
				Usage: domain.TokenUsage{
					InputTokens: 3,
				},
			},
			AutoCharge: llmrequestapp.AutoChargeResult{
				Status: llmrequestapp.
					AutoChargeStatusProcessed,
				ProcessedBatchIDs: []string{
					"billchg_embeddings",
				},
				ChargedAmountCents:  10,
				BillingBalanceCents: &balance,
			},
			Response: ports.ForwardResponse{
				StatusCode: http.StatusOK,
				Headers: map[string][]string{
					"Content-Type": {
						"application/json",
					},
					"X-Provider-Request-ID": {
						"provider-request-1",
					},
					"Connection": {"close"},
					"X-Billing-Amount-Cents": {
						"999999",
					},
				},
				Body: []byte(upstreamBody),
			},
		},
	}

	router, err := NewLLMRouter(
		requests,
		&testRequestIDs{local: requestID},
		4096,
	)
	if err != nil {
		t.Fatalf("NewLLMRouter: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		embeddingsPath,
		strings.NewReader(requestBody),
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_embeddings_test",
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-embeddings")

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}
	if response.Body.String() != upstreamBody {
		t.Fatalf("body=%q", response.Body.String())
	}
	if requests.calls != 1 {
		t.Fatalf("application calls=%d", requests.calls)
	}
	if requests.input.LocalRequestID != requestID ||
		requests.input.RawAPIKey != "sk_embeddings_test" ||
		requests.input.APIFamily !=
			domain.APIFamilyOpenAICompatible ||
		requests.input.EndpointKind !=
			domain.EndpointEmbeddings ||
		string(requests.input.Payload) != requestBody ||
		requests.input.IdempotencyKey == nil ||
		*requests.input.IdempotencyKey != "idem-embeddings" {
		t.Fatalf("application input=%+v", requests.input)
	}

	headers := response.Header()
	if headers.Get("Content-Type") != "application/json" ||
		headers.Get("X-Provider-Request-ID") !=
			"provider-request-1" {
		t.Fatalf("upstream headers=%#v", headers)
	}
	if headers.Get("Connection") != "" {
		t.Fatalf("hop-by-hop header leaked: %#v", headers)
	}

	wantHeaders := map[string]string{
		"X-Local-Request-ID":               requestID,
		"X-Billing-Provider-Type":          "openai",
		"X-Billing-Client-Model":           "embedding-model",
		"X-Billing-Model":                  "openai:embedding-model",
		"X-Billing-Currency":               "RUB",
		"X-Billing-Amount-Cents":           "10",
		"X-Billing-Remaining-Cents":        "10",
		"X-Billing-Input-Tokens":           "3",
		"X-Billing-Cached-Input-Tokens":    "0",
		"X-Billing-Reasoning-Tokens":       "0",
		"X-Billing-Image-Input-Tokens":     "0",
		"X-Billing-Audio-Input-Tokens":     "0",
		"X-Billing-Audio-Output-Tokens":    "0",
		"X-Billing-File-Input-Tokens":      "0",
		"X-Billing-Video-Input-Tokens":     "0",
		"X-Billing-Output-Tokens":          "0",
		"X-Billing-Usage-Completeness":     "detailed",
		"X-Billing-Status":                 "billable",
		"X-Billing-Auto-Charge-Status":     "processed",
		"X-Wallet-Balance-Cents":           "990",
		"X-Wallet-Effective-Balance-Cents": "900",
		"X-Billing-Pending-Cents":          "100",
	}
	for name, want := range wantHeaders {
		if got := headers.Get(name); got != want {
			t.Fatalf(
				"header %s=%q want=%q; all=%#v",
				name,
				got,
				want,
				headers,
			)
		}
	}
	if headers.Get("X-Auto-Charge-Status") != "" ||
		headers.Get("X-Auto-Charge-Charged-Cents") != "" {
		t.Fatalf("legacy auto-charge headers leaked: %#v", headers)
	}
}

var _ LLMRequest = (*testLLMRequests)(nil)
