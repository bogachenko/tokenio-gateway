package llmrequest

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestServiceExecuteImageGenerationCanonicalPipeline(
	t *testing.T,
) {
	var calls []string
	record := func(value string) {
		calls = append(calls, value)
	}

	payload := []byte(`{
		"model":"image-model",
		"prompt":"A red fox in snow",
		"n":2,
		"size":"1536x1024",
		"quality":"high"
	}`)
	input := Input{
		LocalRequestID: "llmreq_images_test",
		RawAPIKey:      "sk_test",
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointImagesGeneration,
		Payload:        payload,
	}

	dependencies := validDependencies(nil)
	dependencies.Authenticator = authenticateFunc(
		func(
			_ context.Context,
			rawAPIKey string,
		) (Principal, error) {
			record("authenticate")
			if rawAPIKey != "sk_test" {
				t.Fatalf("raw API key=%q", rawAPIKey)
			}
			return Principal{
				UserID:               "user-1",
				APIKeyID:             "key-1",
				BillingSubjectUserID: "billing-1",
			}, nil
		},
	)
	dependencies.RequestParser = parseFunc(
		func(
			_ context.Context,
			value ParseInput,
		) (ParsedRequest, error) {
			record("parse")
			if value.APIFamily != domain.APIFamilyOpenAICompatible ||
				value.EndpointKind !=
					domain.EndpointImagesGeneration ||
				!bytes.Equal(value.Payload, payload) {
				t.Fatalf("parse input=%+v", value)
			}
			return ParsedRequest{
				ClientModel: "image-model",
			}, nil
		},
	)
	dependencies.CapabilityDetector = detectFunc(
		func(
			_ context.Context,
			value CapabilityInput,
		) (domain.CapabilitySet, error) {
			record("capabilities")
			if value.EndpointKind !=
				domain.EndpointImagesGeneration ||
				value.ClientModel != "image-model" ||
				!bytes.Equal(value.Payload, payload) {
				t.Fatalf("capability input=%+v", value)
			}
			return domain.CapabilitySet{
				ImagesGeneration: true,
			}, nil
		},
	)
	dependencies.RoutePlanner = planFunc(
		func(
			_ context.Context,
			value RoutePlanInput,
		) (RoutePlan, error) {
			record("route")
			if value.EndpointKind !=
				domain.EndpointImagesGeneration ||
				value.ClientModel != "image-model" ||
				!value.RequestedCapabilities.ImagesGeneration ||
				value.RequestedCapabilities.Chat ||
				value.RequestedCapabilities.Embeddings {
				t.Fatalf("route input=%+v", value)
			}

			plan := validRoutePlan()
			plan.Route.EndpointKind =
				domain.EndpointImagesGeneration
			plan.Route.ClientModel = "image-model"
			plan.Route.ProviderModel =
				"provider-image-model"
			plan.BillingModel = "openai:image-model"
			plan.EstimatedUsage = domain.TokenUsage{
				ImageGenerationUnits: 2,
			}
			plan.EstimatedClientAmountCents = 30
			plan.EstimatedUpstreamCostCents = 20
			return plan, nil
		},
	)
	dependencies.BillingAdmitter = admitFunc(
		func(
			_ context.Context,
			value BillingAdmissionInput,
		) (BillingAdmissionResult, error) {
			record("admission")
			if value.Principal.UserID != "user-1" ||
				value.RequiredReserveCents != 30 ||
				value.Currency != "RUB" {
				t.Fatalf("admission input=%+v", value)
			}
			return BillingAdmissionResult{
				Allowed:               true,
				RemoteBalanceCents:    1000,
				PendingAmountCents:    100,
				EffectiveBalanceCents: 900,
				RequiredReserveCents:  30,
				Currency:              "RUB",
			}, nil
		},
	)
	dependencies.Forwarding = forwardingStageFunc(
		func(
			_ context.Context,
			prepared PreparedRequest,
			admission BillingAdmissionResult,
		) (ForwardedRequest, error) {
			record("forwarding")
			if prepared.EndpointKind !=
				domain.EndpointImagesGeneration ||
				!prepared.RequestedCapabilities.ImagesGeneration ||
				prepared.Plan.Route.EndpointKind !=
					domain.EndpointImagesGeneration ||
				admission.RequiredReserveCents != 30 {
				t.Fatalf(
					"prepared=%+v admission=%+v",
					prepared,
					admission,
				)
			}

			reservation := validReservation(
				reservationInput(prepared),
			)
			return ForwardedRequest{
				Reserved: ReservedRequest{
					Prepared:    prepared,
					Admission:   admission,
					Reservation: reservation,
				},
				Response: ports.ForwardResponse{
					StatusCode: 200,
					Body: []byte(`{
						"created":1710000000,
						"data":[
							{"url":"https://example.test/1.png"},
							{"b64_json":"AAAA"}
						]
					}`),
				},
			}, nil
		},
	)
	dependencies.UsageResolver = usageResolverFunc(
		func(
			_ context.Context,
			value UsageResolutionInput,
		) (UsageResolutionResult, error) {
			record("usage")
			if value.Reserved.Prepared.EndpointKind !=
				domain.EndpointImagesGeneration ||
				value.Response.StatusCode != 200 {
				t.Fatalf("usage input=%+v", value)
			}
			return UsageResolutionResult{
				Usage: domain.TokenUsage{
					ImageGenerationUnits: 2,
				},
				Completeness:      "detailed",
				UpstreamCostCents: 20,
				ClientAmountCents: 30,
				Currency:          "RUB",
			}, nil
		},
	)
	dependencies.Finalizer = finalizerFunc{
		commit: func(
			_ context.Context,
			value FinalizationInput,
		) (FinalizationResult, error) {
			record("finalize")
			if value.ResolvedUsage.Usage.
				ImageGenerationUnits != 2 ||
				value.ResolvedUsage.Usage.InputTokens != 0 ||
				value.ResolvedUsage.Usage.OutputTokens != 0 ||
				value.ResolvedUsage.ClientAmountCents != 30 {
				t.Fatalf("finalization input=%+v", value)
			}
			return FinalizationResult{
				Usage: domain.UsageRecord{
					LocalRequestID: value.Reserved.
						Prepared.LocalRequestID,
					UserID: value.Reserved.Prepared.
						Principal.UserID,
					ProviderType: domain.ProviderOpenAI,
					ClientModel:  "image-model",
					BillingModel: "openai:image-model",
					Currency:     "RUB",
					Usage: domain.TokenUsage{
						ImageGenerationUnits: 2,
					},
					UsageCompleteness:    "detailed",
					ClientAmountCents:    30,
					RemainingAmountCents: 30,
					Status: domain.
						UsageStatusBillable,
				},
			}, nil
		},
		pricingFailed: func(
			context.Context,
			PricingFailureInput,
		) (FinalizationResult, error) {
			t.Fatal("pricing-failure finalizer called")
			return FinalizationResult{}, nil
		},
	}
	dependencies.AutoCharger = autoChargerFunc(
		func(
			_ context.Context,
			value AutoChargeInput,
		) AutoChargeResult {
			record("autocharge")
			if value.Principal.BillingSubjectUserID != "billing-1" ||
				value.FinalUsageRecord.Status !=
					domain.UsageStatusBillable ||
				value.FinalUsageRecord.
					ClientAmountCents != 30 ||
				value.FinalUsageRecord.Usage.
					ImageGenerationUnits != 2 {
				t.Fatalf("auto-charge input=%+v", value)
			}
			balance := int64(970)
			return AutoChargeResult{
				Status: AutoChargeStatusProcessed,
				ProcessedBatchIDs: []string{
					"billchg_images",
				},
				ChargedAmountCents:  30,
				BillingBalanceCents: &balance,
			}
		},
	)

	service, err := NewService(dependencies)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := service.Execute(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	wantCalls := []string{
		"authenticate",
		"parse",
		"capabilities",
		"route",
		"admission",
		"forwarding",
		"usage",
		"finalize",
		"autocharge",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", calls, wantCalls)
	}

	if result.Reserved.Prepared.EndpointKind !=
		domain.EndpointImagesGeneration ||
		!result.Reserved.Prepared.
			RequestedCapabilities.ImagesGeneration ||
		result.ResolvedUsage.Usage.
			ImageGenerationUnits != 2 ||
		result.ResolvedUsage.Usage.InputTokens != 0 ||
		result.ResolvedUsage.Usage.OutputTokens != 0 ||
		result.FinalUsageRecord.Status !=
			domain.UsageStatusBillable ||
		result.FinalUsageRecord.ClientAmountCents != 30 ||
		result.AutoCharge.Status !=
			AutoChargeStatusProcessed ||
		result.AutoCharge.ChargedAmountCents != 30 ||
		result.AutoCharge.BillingBalanceCents == nil ||
		*result.AutoCharge.BillingBalanceCents != 970 ||
		len(result.Response.Body) == 0 {
		t.Fatalf("result=%+v", result)
	}
}
