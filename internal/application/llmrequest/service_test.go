package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
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

func TestServicePrepareExecutesCanonicalOrder(t *testing.T) {
	var calls []string
	service := mustService(t, validDependencies(&calls))
	input := validInput()

	prepared, err := service.Prepare(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	wantCalls := []string{
		"authenticate",
		"parse",
		"capabilities",
		"route",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if prepared.LocalRequestID != input.LocalRequestID ||
		prepared.Principal.UserID != "user-1" ||
		prepared.ClientModel != "model-1" ||
		!prepared.RequestedCapabilities.Chat ||
		prepared.Plan.Route.ID != "route-1" ||
		!bytes.Equal(prepared.Payload, input.Payload) {
		t.Fatalf("prepared = %+v", prepared)
	}
}

func TestServicePrepareStopsAtFirstFailedStage(t *testing.T) {
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

			_, err := service.Prepare(
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

func TestServicePrepareIsolatesPayloadBetweenStages(t *testing.T) {
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
	prepared, err := service.Prepare(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !bytes.Equal(input.Payload, original) {
		t.Fatalf(
			"caller payload = %q, want %q",
			input.Payload,
			original,
		)
	}
	if !bytes.Equal(prepared.Payload, original) {
		t.Fatalf(
			"prepared payload = %q, want %q",
			prepared.Payload,
			original,
		)
	}
}

func TestServicePrepareRejectsInvalidRoutePlan(t *testing.T) {
	dependencies := validDependencies(nil)
	dependencies.RoutePlanner = planFunc(
		func(
			context.Context,
			RoutePlanInput,
		) (RoutePlan, error) {
			plan := validRoutePlan()
			plan.Route.ClientModel = "other-model"
			return plan, nil
		},
	)

	service := mustService(t, dependencies)
	_, err := service.Prepare(
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
