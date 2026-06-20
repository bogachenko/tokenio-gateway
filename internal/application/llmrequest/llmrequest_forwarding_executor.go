package llmrequest

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestForwardingExecutor struct {
	secrets              ports.SecretResolver
	factory              ports.ForwardingAdapterFactory
	maxResponseBodyBytes int64
}

var _ ForwardingExecutor = (*LLMRequestForwardingExecutor)(nil)

func NewLLMRequestForwardingExecutor(
	secrets ports.SecretResolver,
	factory ports.ForwardingAdapterFactory,
	maxResponseBodyBytes int64,
) (*LLMRequestForwardingExecutor, error) {
	if secrets == nil || factory == nil || maxResponseBodyBytes <= 0 {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestForwardingExecutor{secrets: secrets, factory: factory, maxResponseBodyBytes: maxResponseBodyBytes}, nil
}

func (e *LLMRequestForwardingExecutor) Forward(ctx context.Context, input ForwardingExecutionInput) (ForwardingExecutionResult, error) {
	if e == nil || e.secrets == nil || e.factory == nil || e.maxResponseBodyBytes <= 0 {
		return ForwardingExecutionResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return ForwardingExecutionResult{}, fmt.Errorf("%w: nil forwarding executor context", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return ForwardingExecutionResult{}, err
	}

	route := input.Prepared.Plan.Route
	reseller := input.Prepared.Plan.Reseller
	if strings.TrimSpace(reseller.APIKeyEnv) == "" ||
		route.ResellerID != reseller.ID ||
		route.ProviderType != reseller.ProviderType ||
		strings.TrimSpace(input.Prepared.UpstreamPath) == "" ||
		input.Prepared.Payload == nil {
		return ForwardingExecutionResult{}, fmt.Errorf("%w: invalid forwarding execution input", ErrStageContractViolation)
	}

	secret, err := e.secrets.Resolve(ctx, reseller.APIKeyEnv)
	if err != nil {
		return ForwardingExecutionResult{}, fmt.Errorf("resolve reseller secret: %w", err)
	}
	if strings.TrimSpace(secret) == "" {
		return ForwardingExecutionResult{}, fmt.Errorf("%w: resolved reseller secret is blank", ErrStageContractViolation)
	}

	client, err := e.factory.Build(ports.ForwardingAdapterFactoryInput{
		Route:                route,
		Reseller:             reseller,
		ResellerAPIKey:       secret,
		MaxResponseBodyBytes: e.maxResponseBodyBytes,
	})
	if err != nil {
		return ForwardingExecutionResult{}, fmt.Errorf("build forwarding client: %w", err)
	}

	response, err := client.Forward(ctx, ports.ForwardingClientRequest{
		Route: route,
		Path:  input.Prepared.UpstreamPath,
		Body:  append([]byte(nil), input.Prepared.Payload...),
	})
	if err != nil {
		return ForwardingExecutionResult{}, err
	}
	return ForwardingExecutionResult{Response: cloneExecutorForwardResponse(response)}, nil
}

func cloneExecutorForwardResponse(value ports.ForwardResponse) ports.ForwardResponse {
	headers := make(map[string][]string, len(value.Headers))
	for key, values := range value.Headers {
		headers[key] = append([]string(nil), values...)
	}
	if value.Headers == nil {
		headers = nil
	}
	return ports.ForwardResponse{StatusCode: value.StatusCode, Headers: headers, Body: append([]byte(nil), value.Body...), Usage: value.Usage}
}
