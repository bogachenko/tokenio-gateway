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

func TestLLMRouterImageGenerationPassesThroughResponseAndFinalBillingHeaders(
	t *testing.T,
) {
	const requestID = "llmreq_images_transport"
	const requestBody = `{
		"model":"image-model",
		"prompt":"A red fox in snow",
		"n":2,
		"size":"1536x1024",
		"quality":"high"
	}`
	const upstreamBody = `{
		"created":1710000000,
		"data":[
			{"url":"https://example.test/1.png"},
			{"b64_json":"AAAA"}
		]
	}`

	balance := int64(970)
	requests := &testLLMRequests{
		result: llmrequestapp.ForwardedRequest{
			Reserved: llmrequestapp.ReservedRequest{
				Prepared: llmrequestapp.PreparedRequest{
					LocalRequestID: requestID,
					APIFamily:      domain.APIFamilyOpenAICompatible,
					EndpointKind:   domain.EndpointImagesGeneration,
					ClientModel:    "image-model",
					RequestedCapabilities: domain.CapabilitySet{
						ImagesGeneration: true,
					},
					Plan: llmrequestapp.RoutePlan{
						Route: domain.Route{
							ProviderType: domain.ProviderOpenAI,
							EndpointKind: domain.EndpointImagesGeneration,
						},
						BillingModel: "openai:image-model",
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
					ImageGenerationUnits: 2,
				},
				Completeness:      "detailed",
				UpstreamCostCents: 20,
				ClientAmountCents: 30,
				Currency:          "RUB",
			},
			FinalUsageRecord: domain.UsageRecord{
				LocalRequestID:       requestID,
				ProviderType:         domain.ProviderOpenAI,
				ClientModel:          "image-model",
				BillingModel:         "openai:image-model",
				Currency:             "RUB",
				ClientAmountCents:    30,
				RemainingAmountCents: 30,
				UsageCompleteness:    "detailed",
				Status:               domain.UsageStatusBillable,
				Usage: domain.TokenUsage{
					ImageGenerationUnits: 2,
				},
			},
			AutoCharge: llmrequestapp.AutoChargeResult{
				Status: llmrequestapp.AutoChargeStatusProcessed,
				ProcessedBatchIDs: []string{
					"billchg_images",
				},
				ChargedAmountCents:  30,
				BillingBalanceCents: &balance,
			},
			Response: ports.ForwardResponse{
				StatusCode: http.StatusOK,
				Headers: map[string][]string{
					"Content-Type": {
						"application/json",
					},
					"X-Provider-Request-ID": {
						"provider-image-request-1",
					},
					"Connection": {"close"},
					"X-Billing-Image-Generation-Units": {
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
		imageGenerationsPath,
		strings.NewReader(requestBody),
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_images_test",
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-images")

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
		requests.input.RawAPIKey != "sk_images_test" ||
		requests.input.APIFamily != domain.APIFamilyOpenAICompatible ||
		requests.input.EndpointKind != domain.EndpointImagesGeneration ||
		string(requests.input.Payload) != requestBody ||
		requests.input.IdempotencyKey == nil ||
		*requests.input.IdempotencyKey != "idem-images" {
		t.Fatalf("application input=%+v", requests.input)
	}

	headers := response.Header()
	if headers.Get("Content-Type") != "application/json" ||
		headers.Get("X-Provider-Request-ID") !=
			"provider-image-request-1" {
		t.Fatalf("upstream headers=%#v", headers)
	}
	if headers.Get("Connection") != "" {
		t.Fatalf("hop-by-hop header leaked: %#v", headers)
	}

	wantHeaders := map[string]string{
		"X-Local-Request-ID":               requestID,
		"X-Billing-Provider-Type":          "openai",
		"X-Billing-Client-Model":           "image-model",
		"X-Billing-Model":                  "openai:image-model",
		"X-Billing-Currency":               "RUB",
		"X-Billing-Amount-Cents":           "30",
		"X-Billing-Remaining-Cents":        "30",
		"X-Billing-Input-Tokens":           "0",
		"X-Billing-Cached-Input-Tokens":    "0",
		"X-Billing-Reasoning-Tokens":       "0",
		"X-Billing-Image-Input-Tokens":     "0",
		"X-Billing-Audio-Input-Tokens":     "0",
		"X-Billing-Audio-Output-Tokens":    "0",
		"X-Billing-File-Input-Tokens":      "0",
		"X-Billing-Video-Input-Tokens":     "0",
		"X-Billing-Output-Tokens":          "0",
		"X-Billing-Image-Generation-Units": "2",
		"X-Billing-Usage-Completeness":     "detailed",
		"X-Billing-Status":                 "billable",
		"X-Billing-Auto-Charge-Status":     "processed",
		"X-Wallet-Balance-Cents":           "970",
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
