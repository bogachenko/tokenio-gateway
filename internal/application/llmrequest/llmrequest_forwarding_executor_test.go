package llmrequest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type executorSecretResolverFunc func(context.Context, string) (string, error)

func (function executorSecretResolverFunc) Resolve(ctx context.Context, name string) (string, error) {
	return function(ctx, name)
}

type executorFactoryFunc func(ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error)

func (function executorFactoryFunc) Build(input ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error) {
	return function(input)
}

type executorClientFunc func(context.Context, ports.ForwardingClientRequest) (ports.ForwardResponse, error)

func (function executorClientFunc) Forward(ctx context.Context, input ports.ForwardingClientRequest) (ports.ForwardResponse, error) {
	return function(ctx, input)
}

func TestLLMRequestForwardingExecutorUsesSemanticBoundary(t *testing.T) {
	input := validExecutorInput()
	original := append([]byte(nil), input.Prepared.Payload...)
	var gotSecretName string
	var gotFactory ports.ForwardingAdapterFactoryInput
	var gotClientRouteID string
	var gotClientPath string
	var gotClientBody []byte
	executor := mustLLMRequestForwardingExecutor(t,
		executorSecretResolverFunc(func(_ context.Context, name string) (string, error) { gotSecretName = name; return "resolved-secret", nil }),
		executorFactoryFunc(func(value ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error) {
			gotFactory = value
			return executorClientFunc(func(_ context.Context, request ports.ForwardingClientRequest) (ports.ForwardResponse, error) {
				gotClientRouteID = request.Route.ID
				gotClientPath = request.Path
				gotClientBody = append([]byte(nil), request.Body...)
				request.Body[0] = 'X'
				return ports.ForwardResponse{StatusCode: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)}, nil
			}), nil
		}),
		1024,
	)
	result, err := executor.Forward(context.Background(), input)
	if err != nil { t.Fatalf("Forward: %v", err) }
	if gotSecretName != input.Prepared.Plan.Reseller.APIKeyEnv { t.Fatalf("secret name=%q", gotSecretName) }
	if gotFactory.Route.ID != input.Prepared.Plan.Route.ID || gotFactory.Reseller.ID != input.Prepared.Plan.Reseller.ID || gotFactory.ResellerAPIKey != "resolved-secret" || gotFactory.MaxResponseBodyBytes != 1024 { t.Fatalf("factory input=%+v", gotFactory) }
	if gotClientRouteID != input.Prepared.Plan.Route.ID || gotClientPath != input.Prepared.UpstreamPath || !bytes.Equal(gotClientBody, original) { t.Fatalf("client input route/path/body=%q/%q/%q", gotClientRouteID, gotClientPath, gotClientBody) }
	if !bytes.Equal(input.Prepared.Payload, original) { t.Fatalf("caller payload mutated: %q", input.Prepared.Payload) }
	if result.Response.StatusCode != 200 { t.Fatalf("result=%+v", result) }
}

func TestLLMRequestForwardingExecutorStopsAtSecretFailure(t *testing.T) {
	stageError := errors.New("secret failed")
	executor := mustLLMRequestForwardingExecutor(t,
		executorSecretResolverFunc(func(context.Context, string) (string, error) { return "", stageError }),
		executorFactoryFunc(func(ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error) { t.Fatal("factory must not be called"); return nil, nil }),
		1024,
	)
	_, err := executor.Forward(context.Background(), validExecutorInput())
	if !errors.Is(err, stageError) { t.Fatalf("error=%v", err) }
}

func TestLLMRequestForwardingExecutorRejectsBlankSecret(t *testing.T) {
	executor := mustLLMRequestForwardingExecutor(t,
		executorSecretResolverFunc(func(context.Context, string) (string, error) { return " ", nil }),
		executorFactoryFunc(func(ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error) { t.Fatal("factory must not be called"); return nil, nil }),
		1024,
	)
	_, err := executor.Forward(context.Background(), validExecutorInput())
	if !errors.Is(err, ErrStageContractViolation) { t.Fatalf("error=%v", err) }
}

func TestNewLLMRequestForwardingExecutorRequiresDependencies(t *testing.T) {
	validSecrets := executorSecretResolverFunc(func(context.Context, string) (string, error) { return "secret", nil })
	validFactory := executorFactoryFunc(func(ports.ForwardingAdapterFactoryInput) (ports.ForwardingClient, error) { return executorClientFunc(func(context.Context, ports.ForwardingClientRequest) (ports.ForwardResponse, error) { return ports.ForwardResponse{}, nil }), nil })
	tests := []struct { name string; secrets ports.SecretResolver; factory ports.ForwardingAdapterFactory; limit int64 }{{name: "secrets", factory: validFactory, limit: 1}, {name: "factory", secrets: validSecrets, limit: 1}, {name: "limit", secrets: validSecrets, factory: validFactory}}
	for _, test := range tests { t.Run(test.name, func(t *testing.T) { _, err := NewLLMRequestForwardingExecutor(test.secrets, test.factory, test.limit); if !errors.Is(err, ErrDependencyRequired) { t.Fatalf("error=%v", err) } }) }
}

func mustLLMRequestForwardingExecutor(t *testing.T, secrets ports.SecretResolver, factory ports.ForwardingAdapterFactory, limit int64) *LLMRequestForwardingExecutor {
	t.Helper()
	executor, err := NewLLMRequestForwardingExecutor(secrets, factory, limit)
	if err != nil { t.Fatalf("NewLLMRequestForwardingExecutor: %v", err) }
	return executor
}

func validExecutorInput() ForwardingExecutionInput {
	route := domain.Route{ID: "route-1", ResellerID: "reseller-1", ProviderType: domain.ProviderOpenAI, APIFamily: domain.APIFamilyOpenAICompatible, EndpointKind: domain.EndpointChat, ClientModel: "model-1", ProviderModel: "model-1", ModelRewritePolicy: domain.ModelRewritePolicyNone, Capabilities: domain.CapabilitySet{Chat: true}}
	reseller := domain.Reseller{ID: "reseller-1", ProviderType: domain.ProviderOpenAI, BaseURL: "https://provider.example/api", APIKeyEnv: "RESELLER_API_KEY"}
	prepared := PreparedRequest{LocalRequestID: "llmreq_test", Principal: Principal{UserID: "user-1", APIKeyID: "key-1", BillingSubjectUserID: "billing-1"}, APIFamily: route.APIFamily, EndpointKind: route.EndpointKind, ClientModel: route.ClientModel, RequestedCapabilities: route.Capabilities, UpstreamPath: "/v1/chat/completions", Payload: []byte(`{"model":"model-1"}`), Plan: RoutePlan{Route: route, Reseller: reseller, BillingModel: "model-1", EstimatedUsage: domain.TokenUsage{InputTokens: 1}, EstimatedClientAmountCents: 20, EstimatedUpstreamCostCents: 10, Currency: "RUB", Confidence: "estimated"}}
	return ForwardingExecutionInput{Prepared: prepared, Admission: BillingAdmissionResult{Allowed: true, RequiredReserveCents: 20, Currency: "RUB"}, Reservation: ReservationResult{Disposition: ReservationDispositionCreated, Usage: domain.UsageRecord{LocalRequestID: "llmreq_test", SelectedRouteID: "route-1", Status: domain.UsageStatusReserved}, Reseller: &reseller}}
}
