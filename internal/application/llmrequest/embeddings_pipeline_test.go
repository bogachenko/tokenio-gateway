package llmrequest

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestServiceExecuteEmbeddingsCanonicalPipeline(
	t *testing.T,
) {
	var calls []string
	record := func(value string) {
		calls = append(calls, value)
	}

	payload := []byte(
		`{"model":"embedding-model","input":["ab","c"]}`,
	)
	input := Input{
		LocalRequestID: "llmreq_embeddings_test",
		RawAPIKey:      "sk_test",
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointEmbeddings,
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
				value.EndpointKind != domain.EndpointEmbeddings ||
				!bytes.Equal(value.Payload, payload) {
				t.Fatalf("parse input=%+v", value)
			}
			return ParsedRequest{
				ClientModel: "embedding-model",
			}, nil
		},
	)
	dependencies.CapabilityDetector = detectFunc(
		func(
			_ context.Context,
			value CapabilityInput,
		) (domain.CapabilitySet, error) {
			record("capabilities")
			if value.EndpointKind != domain.EndpointEmbeddings ||
				value.ClientModel != "embedding-model" ||
				!bytes.Equal(value.Payload, payload) {
				t.Fatalf("capability input=%+v", value)
			}
			return domain.CapabilitySet{
				Embeddings: true,
			}, nil
		},
	)
	dependencies.RoutePlanner = planFunc(
		func(
			_ context.Context,
			value RoutePlanInput,
		) (RoutePlan, error) {
			record("route")
			if value.EndpointKind != domain.EndpointEmbeddings ||
				value.ClientModel != "embedding-model" ||
				!value.RequestedCapabilities.Embeddings ||
				value.RequestedCapabilities.Chat {
				t.Fatalf("route input=%+v", value)
			}

			plan := validRoutePlan()
			plan.Route.EndpointKind = domain.EndpointEmbeddings
			plan.Route.ClientModel = "embedding-model"
			plan.Route.ProviderModel = "provider-embedding-model"
			plan.BillingModel = "openai:embedding-model"
			plan.EstimatedUsage = domain.TokenUsage{
				InputTokens: 3,
			}
			plan.EstimatedClientAmountCents = 12
			plan.EstimatedUpstreamCostCents = 8
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
				value.RequiredReserveCents != 12 ||
				value.Currency != "RUB" {
				t.Fatalf("admission input=%+v", value)
			}
			return BillingAdmissionResult{
				Allowed:               true,
				RemoteBalanceCents:    1000,
				PendingAmountCents:    100,
				EffectiveBalanceCents: 900,
				RequiredReserveCents:  12,
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
			if prepared.EndpointKind != domain.EndpointEmbeddings ||
				!prepared.RequestedCapabilities.Embeddings ||
				prepared.Plan.Route.EndpointKind !=
					domain.EndpointEmbeddings ||
				admission.RequiredReserveCents != 12 {
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
				domain.EndpointEmbeddings ||
				value.Response.StatusCode != 200 {
				t.Fatalf("usage input=%+v", value)
			}
			return UsageResolutionResult{
				Usage: domain.TokenUsage{
					InputTokens: 3,
				},
				Completeness:          "detailed",
				UpstreamCostCents:     8,
				ClientAmountCents:     10,
				Currency:              "RUB",
				ProviderResponseModel: "provider-embedding-model",
			}, nil
		},
	)
	dependencies.Finalizer = finalizerFunc{
		commit: func(
			_ context.Context,
			value FinalizationInput,
		) (FinalizationResult, error) {
			record("finalize")
			if value.ResolvedUsage.Usage.InputTokens != 3 ||
				value.ResolvedUsage.Usage.OutputTokens != 0 ||
				value.ResolvedUsage.ClientAmountCents != 10 {
				t.Fatalf("finalization input=%+v", value)
			}
			return FinalizationResult{
				Usage: domain.UsageRecord{
					LocalRequestID: value.Reserved.
						Prepared.LocalRequestID,
					UserID: value.Reserved.Prepared.
						Principal.UserID,
					ProviderType: domain.ProviderOpenAI,
					ClientModel:  "embedding-model",
					BillingModel: "openai:embedding-model",
					Currency:     "RUB",
					Usage: domain.TokenUsage{
						InputTokens: 3,
					},
					UsageCompleteness:    "detailed",
					ClientAmountCents:    10,
					RemainingAmountCents: 10,
					Status:               domain.UsageStatusBillable,
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
				value.FinalUsageRecord.ClientAmountCents != 10 {
				t.Fatalf("auto-charge input=%+v", value)
			}
			balance := int64(990)
			return AutoChargeResult{
				Status: AutoChargeStatusProcessed,
				ProcessedBatchIDs: []string{
					"billchg_embeddings",
				},
				ChargedAmountCents:  10,
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
		domain.EndpointEmbeddings ||
		!result.Reserved.Prepared.RequestedCapabilities.Embeddings ||
		result.ResolvedUsage.Usage.InputTokens != 3 ||
		result.ResolvedUsage.Usage.OutputTokens != 0 ||
		result.FinalUsageRecord.Status != domain.UsageStatusBillable ||
		result.FinalUsageRecord.ClientAmountCents != 10 ||
		result.AutoCharge.Status != AutoChargeStatusProcessed ||
		result.AutoCharge.ChargedAmountCents != 10 ||
		result.AutoCharge.BillingBalanceCents == nil ||
		*result.AutoCharge.BillingBalanceCents != 990 ||
		len(result.Response.Body) == 0 {
		t.Fatalf("result=%+v", result)
	}
}
