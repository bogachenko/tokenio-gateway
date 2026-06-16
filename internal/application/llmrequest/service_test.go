package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type authenticateFunc func(
	context.Context,
	string,
) (Principal, error)

func (function authenticateFunc) Authenticate(
	ctx context.Context,
	rawAPIKey string,
) (Principal, error) {
	return function(ctx, rawAPIKey)
}

type parseFunc func(
	context.Context,
	ParseInput,
) (ParsedRequest, error)

func (function parseFunc) Parse(
	ctx context.Context,
	input ParseInput,
) (ParsedRequest, error) {
	return function(ctx, input)
}

type detectFunc func(
	context.Context,
	CapabilityInput,
) (domain.CapabilitySet, error)

func (function detectFunc) Detect(
	ctx context.Context,
	input CapabilityInput,
) (domain.CapabilitySet, error) {
	return function(ctx, input)
}

type planFunc func(
	context.Context,
	RoutePlanInput,
) (RoutePlan, error)

func (function planFunc) Plan(
	ctx context.Context,
	input RoutePlanInput,
) (RoutePlan, error) {
	return function(ctx, input)
}

type admitFunc func(
	context.Context,
	BillingAdmissionInput,
) (BillingAdmissionResult, error)

func (function admitFunc) Admit(
	ctx context.Context,
	input BillingAdmissionInput,
) (BillingAdmissionResult, error) {
	return function(ctx, input)
}

type reserveFunc func(
	context.Context,
	ReservationInput,
) (ReservationResult, error)

func (function reserveFunc) Reserve(
	ctx context.Context,
	input ReservationInput,
) (ReservationResult, error) {
	return function(ctx, input)
}

type forwardingStageFunc func(
	context.Context,
	PreparedRequest,
	BillingAdmissionResult,
) (ForwardedRequest, error)

type usageResolverFunc func(
	context.Context,
	UsageResolutionInput,
) (UsageResolutionResult, error)

func (function usageResolverFunc) Resolve(
	ctx context.Context,
	input UsageResolutionInput,
) (UsageResolutionResult, error) {
	return function(ctx, input)
}

type finalizerFunc struct {
	commit func(
		context.Context,
		FinalizationInput,
	) (FinalizationResult, error)
	pricingFailed func(
		context.Context,
		PricingFailureInput,
	) (FinalizationResult, error)
}

func (function finalizerFunc) Commit(
	ctx context.Context,
	input FinalizationInput,
) (FinalizationResult, error) {
	return function.commit(ctx, input)
}

func (function finalizerFunc) MarkPricingFailed(
	ctx context.Context,
	input PricingFailureInput,
) (FinalizationResult, error) {
	return function.pricingFailed(ctx, input)
}

type autoChargerFunc func(
	context.Context,
	AutoChargeInput,
) AutoChargeResult

func (function autoChargerFunc) Run(
	ctx context.Context,
	input AutoChargeInput,
) AutoChargeResult {
	return function(ctx, input)
}

func (function forwardingStageFunc) Execute(
	ctx context.Context,
	prepared PreparedRequest,
	admission BillingAdmissionResult,
) (ForwardedRequest, error) {
	return function(ctx, prepared, admission)
}

func TestNewServiceRequiresEveryDependency(t *testing.T) {
	valid := validDependencies(nil)

	tests := []struct {
		name   string
		mutate func(Dependencies) Dependencies
	}{
		{
			name: "authenticator",
			mutate: func(value Dependencies) Dependencies {
				value.Authenticator = nil
				return value
			},
		},
		{
			name: "request parser",
			mutate: func(value Dependencies) Dependencies {
				value.RequestParser = nil
				return value
			},
		},
		{
			name: "capability detector",
			mutate: func(value Dependencies) Dependencies {
				value.CapabilityDetector = nil
				return value
			},
		},
		{
			name: "route planner",
			mutate: func(value Dependencies) Dependencies {
				value.RoutePlanner = nil
				return value
			},
		},
		{
			name: "billing admitter",
			mutate: func(value Dependencies) Dependencies {
				value.BillingAdmitter = nil
				return value
			},
		},
		{
			name: "forwarding stage",
			mutate: func(value Dependencies) Dependencies {
				value.Forwarding = nil
				return value
			},
		},
		{
			name: "usage resolver",
			mutate: func(value Dependencies) Dependencies {
				value.UsageResolver = nil
				return value
			},
		},
		{
			name: "finalizer",
			mutate: func(value Dependencies) Dependencies {
				value.Finalizer = nil
				return value
			},
		},
		{
			name: "auto charger",
			mutate: func(value Dependencies) Dependencies {
				value.AutoCharger = nil
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewService(test.mutate(valid))
			if !errors.Is(err, ErrDependencyRequired) {
				t.Fatalf(
					"error = %v, want dependency required",
					err,
				)
			}
		})
	}
}

func TestServiceExecuteExecutesCanonicalOrder(t *testing.T) {
	var calls []string
	service := mustService(t, validDependencies(&calls))
	input := validInput()

	reserved, err := service.Execute(
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
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if reserved.Reserved.Prepared.LocalRequestID !=
		input.LocalRequestID ||
		reserved.Reserved.Prepared.Principal.UserID != "user-1" ||
		reserved.Reserved.Prepared.ClientModel != "model-1" ||
		!reserved.Reserved.Prepared.RequestedCapabilities.Chat ||
		reserved.Reserved.Prepared.Plan.Route.ID != "route-1" ||
		!bytes.Equal(
			reserved.Reserved.Prepared.Payload,
			input.Payload,
		) ||
		!reserved.Reserved.Admission.Allowed ||
		reserved.Reserved.Reservation.Disposition !=
			ReservationDispositionCreated ||
		reserved.Reserved.Reservation.Usage.Status !=
			domain.UsageStatusReserved ||
		reserved.Response.StatusCode != 200 ||
		reserved.ResolvedUsage.Completeness != "detailed" ||
		reserved.ResolvedUsage.ClientAmountCents != 15 ||
		reserved.FinalUsageRecord.Status !=
			domain.UsageStatusBillable ||
		reserved.AutoCharge.Status !=
			AutoChargeStatusDeferred {
		t.Fatalf("forwarded = %+v", reserved)
	}
}

func TestServiceExecuteRejectsNonzeroUsageWithZeroFinalAmount(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*UsageResolutionResult)
	}{
		{
			name: "zero upstream cost",
			mutate: func(value *UsageResolutionResult) {
				value.UpstreamCostCents = 0
			},
		},
		{
			name: "zero client amount",
			mutate: func(value *UsageResolutionResult) {
				value.ClientAmountCents = 0
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			dependencies := validDependencies(&calls)
			dependencies.UsageResolver = usageResolverFunc(
				func(
					context.Context,
					UsageResolutionInput,
				) (UsageResolutionResult, error) {
					calls = append(calls, "usage")
					result := UsageResolutionResult{
						Usage: domain.TokenUsage{
							InputTokens:  10,
							OutputTokens: 5,
						},
						Completeness:      "detailed",
						UpstreamCostCents: 10,
						ClientAmountCents: 15,
						Currency:          "RUB",
					}
					test.mutate(&result)
					return result, nil
				},
			)

			service := mustService(t, dependencies)
			result, err := service.Execute(
				context.Background(),
				validInput(),
			)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if result.Response.StatusCode != 200 ||
				result.FinalUsageRecord.Status != domain.UsageStatusPricingFailed {
				t.Fatalf("result = %+v", result)
			}

			wantCalls := []string{
				"authenticate",
				"parse",
				"capabilities",
				"route",
				"admission",
				"forwarding",
				"usage",
				"pricing_failed",
			}
			if !reflect.DeepEqual(calls, wantCalls) {
				t.Fatalf(
					"calls = %#v, want %#v",
					calls,
					wantCalls,
				)
			}
		})
	}
}

func TestServiceExecuteReturnsSuccessfulUpstreamWhenUsageResolutionFails(
	t *testing.T,
) {
	var calls []string
	dependencies := validDependencies(&calls)
	dependencies.UsageResolver = usageResolverFunc(
		func(context.Context, UsageResolutionInput) (UsageResolutionResult, error) {
			calls = append(calls, "usage")
			return UsageResolutionResult{}, errors.New("usage resolution failed")
		},
	)

	service := mustService(t, dependencies)
	result, err := service.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Response.StatusCode != 200 ||
		string(result.Response.Body) != `{"ok":true}` ||
		result.FinalUsageRecord.Status != domain.UsageStatusPricingFailed {
		t.Fatalf("result = %+v", result)
	}
	if result.AutoCharge.Status != "" {
		t.Fatalf("auto charge unexpectedly ran: %+v", result.AutoCharge)
	}
	wantCalls := []string{
		"authenticate", "parse", "capabilities", "route",
		"admission", "forwarding", "usage", "pricing_failed",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestServiceExecuteStopsAtFirstFailedStage(t *testing.T) {
	stageError := errors.New("stage failed")

	tests := []struct {
		name      string
		failStage string
		wantCalls []string
	}{
		{
			name:      "authentication",
			failStage: "authenticate",
			wantCalls: []string{"authenticate"},
		},
		{
			name:      "parser",
			failStage: "parse",
			wantCalls: []string{"authenticate", "parse"},
		},
		{
			name:      "capability detector",
			failStage: "capabilities",
			wantCalls: []string{
				"authenticate",
				"parse",
				"capabilities",
			},
		},
		{
			name:      "route planner",
			failStage: "route",
			wantCalls: []string{
				"authenticate",
				"parse",
				"capabilities",
				"route",
			},
		},
		{
			name:      "billing admission",
			failStage: "admission",
			wantCalls: []string{
				"authenticate",
				"parse",
				"capabilities",
				"route",
				"admission",
			},
		},
		{
			name:      "forwarding stage",
			failStage: "forwarding",
			wantCalls: []string{
				"authenticate",
				"parse",
				"capabilities",
				"route",
				"admission",
				"forwarding",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			dependencies := validDependencies(&calls)
			dependencies = failingDependencies(
				dependencies,
				&calls,
				test.failStage,
				stageError,
			)
			service := mustService(t, dependencies)

			_, err := service.Execute(
				context.Background(),
				validInput(),
			)
			if !errors.Is(err, stageError) {
				t.Fatalf("error = %v, want stage error", err)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf(
					"calls = %#v, want %#v",
					calls,
					test.wantCalls,
				)
			}
		})
	}
}

func TestServiceExecuteDoesNotCallForwardingWhenAdmissionDenied(
	t *testing.T,
) {
	var calls []string
	dependencies := validDependencies(&calls)
	dependencies.BillingAdmitter = admitFunc(
		func(
			_ context.Context,
			input BillingAdmissionInput,
		) (BillingAdmissionResult, error) {
			calls = append(calls, "admission")
			result := validAdmission(input)
			result.Allowed = false
			return result, nil
		},
	)

	service := mustService(t, dependencies)
	_, err := service.Execute(
		context.Background(),
		validInput(),
	)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
	wantCalls := []string{
		"authenticate",
		"parse",
		"capabilities",
		"route",
		"admission",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestServiceExecuteIsolatesPayloadBetweenStages(t *testing.T) {
	input := validInput()
	original := append([]byte(nil), input.Payload...)

	dependencies := validDependencies(nil)
	dependencies.RequestParser = parseFunc(
		func(
			_ context.Context,
			value ParseInput,
		) (ParsedRequest, error) {
			value.Payload[0] = 'X'
			return ParsedRequest{
				ClientModel: "model-1",
			}, nil
		},
	)
	dependencies.CapabilityDetector = detectFunc(
		func(
			_ context.Context,
			value CapabilityInput,
		) (domain.CapabilitySet, error) {
			if !bytes.Equal(value.Payload, original) {
				t.Fatalf(
					"capability payload = %q, want %q",
					value.Payload,
					original,
				)
			}
			value.Payload[0] = 'Y'
			return domain.CapabilitySet{Chat: true}, nil
		},
	)
	dependencies.RoutePlanner = planFunc(
		func(
			_ context.Context,
			value RoutePlanInput,
		) (RoutePlan, error) {
			if !bytes.Equal(value.Payload, original) {
				t.Fatalf(
					"route payload = %q, want %q",
					value.Payload,
					original,
				)
			}
			value.Payload[0] = 'Z'
			return validRoutePlan(), nil
		},
	)

	service := mustService(t, dependencies)
	reserved, err := service.Execute(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Equal(input.Payload, original) {
		t.Fatalf(
			"caller payload = %q, want %q",
			input.Payload,
			original,
		)
	}
	if !bytes.Equal(
		reserved.Reserved.Prepared.Payload,
		original,
	) {
		t.Fatalf(
			"prepared payload = %q, want %q",
			reserved.Reserved.Prepared.Payload,
			original,
		)
	}
}

func TestServiceExecuteRejectsInvalidRoutePlan(t *testing.T) {
	dependencies := validDependencies(nil)
	dependencies.RoutePlanner = planFunc(
		func(
			context.Context,
			RoutePlanInput,
		) (RoutePlan, error) {
			plan := validRoutePlan()
			plan.BillingModel = ""
			return plan, nil
		},
	)

	service := mustService(t, dependencies)
	_, err := service.Execute(
		context.Background(),
		validInput(),
	)
	if !errors.Is(err, ErrStageContractViolation) {
		t.Fatalf(
			"error = %v, want stage contract violation",
			err,
		)
	}
}

func validDependencies(
	calls *[]string,
) Dependencies {
	record := func(value string) {
		if calls != nil {
			*calls = append(*calls, value)
		}
	}

	return Dependencies{
		Authenticator: authenticateFunc(
			func(
				_ context.Context,
				rawAPIKey string,
			) (Principal, error) {
				record("authenticate")
				if rawAPIKey != "sk_test" {
					return Principal{}, errors.New(
						"unexpected API key",
					)
				}
				return Principal{
					UserID:               "user-1",
					APIKeyID:             "key-1",
					BillingSubjectUserID: "billing-1",
				}, nil
			},
		),
		RequestParser: parseFunc(
			func(
				_ context.Context,
				input ParseInput,
			) (ParsedRequest, error) {
				record("parse")
				if input.APIFamily !=
					domain.APIFamilyOpenAICompatible ||
					input.EndpointKind != domain.EndpointChat {
					return ParsedRequest{}, errors.New(
						"unexpected parse input",
					)
				}
				return ParsedRequest{
					ClientModel: "model-1",
				}, nil
			},
		),
		CapabilityDetector: detectFunc(
			func(
				_ context.Context,
				input CapabilityInput,
			) (domain.CapabilitySet, error) {
				record("capabilities")
				if input.ClientModel != "model-1" {
					return domain.CapabilitySet{},
						errors.New(
							"unexpected capability input",
						)
				}
				return domain.CapabilitySet{
					Chat: true,
				}, nil
			},
		),
		RoutePlanner: planFunc(
			func(
				_ context.Context,
				input RoutePlanInput,
			) (RoutePlan, error) {
				record("route")
				if input.Principal.UserID != "user-1" ||
					input.ClientModel != "model-1" ||
					!input.RequestedCapabilities.Chat {
					return RoutePlan{}, errors.New(
						"unexpected route input",
					)
				}
				return validRoutePlan(), nil
			},
		),
		BillingAdmitter: admitFunc(
			func(
				_ context.Context,
				input BillingAdmissionInput,
			) (BillingAdmissionResult, error) {
				record("admission")
				if input.Principal.UserID != "user-1" ||
					input.RequiredReserveCents != 20 ||
					input.Currency != "RUB" {
					return BillingAdmissionResult{},
						errors.New(
							"unexpected admission input",
						)
				}
				return validAdmission(input), nil
			},
		),
		Forwarding: forwardingStageFunc(
			func(
				_ context.Context,
				prepared PreparedRequest,
				admission BillingAdmissionResult,
			) (ForwardedRequest, error) {
				record("forwarding")
				if prepared.LocalRequestID != "llmreq_test" ||
					prepared.Principal.UserID != "user-1" ||
					prepared.Plan.Route.ID != "route-1" ||
					!admission.Allowed {
					return ForwardedRequest{},
						errors.New(
							"unexpected forwarding input",
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
						Body:       []byte(`{"ok":true}`),
					},
				}, nil
			},
		),
		UsageResolver: usageResolverFunc(
			func(
				_ context.Context,
				input UsageResolutionInput,
			) (UsageResolutionResult, error) {
				record("usage")
				if input.Reserved.Prepared.LocalRequestID !=
					"llmreq_test" ||
					input.Response.StatusCode != 200 {
					return UsageResolutionResult{},
						errors.New(
							"unexpected usage input",
						)
				}
				return UsageResolutionResult{
					Usage: domain.TokenUsage{
						InputTokens:  10,
						OutputTokens: 5,
					},
					Completeness:      "detailed",
					UpstreamCostCents: 10,
					ClientAmountCents: 15,
					Currency:          "RUB",
				}, nil
			},
		),
		Finalizer: finalizerFunc{
			commit: func(
				_ context.Context,
				input FinalizationInput,
			) (FinalizationResult, error) {
				record("finalize")
				return FinalizationResult{
					Usage: domain.UsageRecord{
						LocalRequestID: input.Reserved.
							Prepared.LocalRequestID,
						UserID: input.Reserved.Prepared.
							Principal.UserID,
						Currency: "RUB",
						Status: domain.
							UsageStatusBillable,
					},
				}, nil
			},
			pricingFailed: func(
				_ context.Context,
				input PricingFailureInput,
			) (FinalizationResult, error) {
				record("pricing_failed")
				return FinalizationResult{
					Usage: domain.UsageRecord{
						LocalRequestID: input.Reserved.
							Prepared.LocalRequestID,
						Status: domain.
							UsageStatusPricingFailed,
					},
				}, nil
			},
		},
		AutoCharger: autoChargerFunc(
			func(
				_ context.Context,
				input AutoChargeInput,
			) AutoChargeResult {
				record("autocharge")
				if input.FinalUsageRecord.Status !=
					domain.UsageStatusBillable {
					return AutoChargeResult{
						Status: AutoChargeStatusFailed,
					}
				}
				return AutoChargeResult{
					Status: AutoChargeStatusDeferred,
				}
			},
		),
	}
}

func failingDependencies(
	value Dependencies,
	calls *[]string,
	stage string,
	stageError error,
) Dependencies {
	record := func() {
		*calls = append(*calls, stage)
	}

	switch stage {
	case "authenticate":
		value.Authenticator = authenticateFunc(
			func(
				context.Context,
				string,
			) (Principal, error) {
				record()
				return Principal{}, stageError
			},
		)
	case "parse":
		value.RequestParser = parseFunc(
			func(
				context.Context,
				ParseInput,
			) (ParsedRequest, error) {
				record()
				return ParsedRequest{}, stageError
			},
		)
	case "capabilities":
		value.CapabilityDetector = detectFunc(
			func(
				context.Context,
				CapabilityInput,
			) (domain.CapabilitySet, error) {
				record()
				return domain.CapabilitySet{}, stageError
			},
		)
	case "route":
		value.RoutePlanner = planFunc(
			func(
				context.Context,
				RoutePlanInput,
			) (RoutePlan, error) {
				record()
				return RoutePlan{}, stageError
			},
		)
	case "admission":
		value.BillingAdmitter = admitFunc(
			func(
				context.Context,
				BillingAdmissionInput,
			) (BillingAdmissionResult, error) {
				record()
				return BillingAdmissionResult{}, stageError
			},
		)
	case "forwarding":
		value.Forwarding = forwardingStageFunc(
			func(
				context.Context,
				PreparedRequest,
				BillingAdmissionResult,
			) (ForwardedRequest, error) {
				record()
				return ForwardedRequest{}, stageError
			},
		)
	case "usage":
		value.UsageResolver = usageResolverFunc(
			func(
				context.Context,
				UsageResolutionInput,
			) (UsageResolutionResult, error) {
				record()
				return UsageResolutionResult{}, stageError
			},
		)
	default:
		panic("unknown stage")
	}
	return value
}

func validInput() Input {
	idempotencyKey := "idem-1"
	return Input{
		LocalRequestID: "llmreq_test",
		RawAPIKey:      "sk_test",
		IdempotencyKey: &idempotencyKey,
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointChat,
		Payload:        []byte(`{"model":"model-1"}`),
	}
}

func validRoutePlan() RoutePlan {
	return RoutePlan{
		Route: domain.Route{
			ID:            "route-1",
			ResellerID:    "reseller-1",
			ProviderType:  domain.ProviderOpenAI,
			APIFamily:     domain.APIFamilyOpenAICompatible,
			EndpointKind:  domain.EndpointChat,
			ClientModel:   "model-1",
			ProviderModel: "model-1",
			Enabled:       true,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
			Enabled:      true,
		},
		Price: domain.RoutePrice{
			RouteID:  "route-1",
			Currency: "RUB",
			Enabled:  true,
		},
		BillingModel: "openai:model-1",
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		EstimatedClientAmountCents: 20,
		EstimatedUpstreamCostCents: 10,
		Currency:                   "RUB",
		Confidence:                 "conservative",
	}
}

func validAdmission(
	input BillingAdmissionInput,
) BillingAdmissionResult {
	return BillingAdmissionResult{
		Allowed:               true,
		RemoteBalanceCents:    1000,
		PendingAmountCents:    100,
		EffectiveBalanceCents: 900,
		RequiredReserveCents:  input.RequiredReserveCents,
		Currency:              input.Currency,
	}
}

func validReservation(
	input ReservationInput,
) ReservationResult {
	reservedAt := time.Date(
		2026,
		time.June,
		14,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	idempotencyKey := ""
	if input.IdempotencyKey != nil {
		idempotencyKey = *input.IdempotencyKey
	}
	reseller := input.Reseller
	reseller.ReservedCents = 10
	return ReservationResult{
		Disposition: ReservationDispositionCreated,
		Usage: domain.UsageRecord{
			LocalRequestID:     input.LocalRequestID,
			IdempotencyKey:     idempotencyKey,
			UserID:             input.Principal.UserID,
			APIKeyID:           input.Principal.APIKeyID,
			APIFamily:          input.APIFamily,
			EndpointKind:       input.EndpointKind,
			ClientModel:        input.ClientModel,
			BillingModel:       input.BillingModel,
			SelectedRouteID:    input.Route.ID,
			SelectedResellerID: input.Reseller.ID,
			ProviderType:       input.Route.ProviderType,
			ProviderModel:      input.Route.ProviderModel,
			EstimatedUsage:     input.EstimatedUsage,
			EstimatedClientAmountCents: input.
				EstimatedClientAmountCents,
			EstimatedUpstreamCostCents: input.
				EstimatedUpstreamCostCents,
			Currency:          input.Currency,
			UsageCompleteness: "missing",
			Status:            domain.UsageStatusReserved,
			CreatedAt:         reservedAt,
			ReservedAt:        &reservedAt,
			UpdatedAt:         reservedAt,
		},
		Reseller: &reseller,
	}
}

func mustService(
	t *testing.T,
	dependencies Dependencies,
) *Service {
	t.Helper()

	service, err := NewService(dependencies)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}
