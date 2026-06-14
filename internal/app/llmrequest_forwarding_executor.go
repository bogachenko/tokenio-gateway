package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestForwardingExecutor struct {
	secrets              ports.SecretResolver
	factory              ports.ForwardingAdapterFactory
	maxResponseBodyBytes int64
}

var _ llmrequest.ForwardingExecutor = (*LLMRequestForwardingExecutor)(nil)

func NewLLMRequestForwardingExecutor(
	secrets ports.SecretResolver,
	factory ports.ForwardingAdapterFactory,
	maxResponseBodyBytes int64,
) (*LLMRequestForwardingExecutor, error) {
	if secrets == nil ||
		factory == nil ||
		maxResponseBodyBytes <= 0 {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestForwardingExecutor{
		secrets:              secrets,
		factory:              factory,
		maxResponseBodyBytes: maxResponseBodyBytes,
	}, nil
}

func (e *LLMRequestForwardingExecutor) Forward(
	ctx context.Context,
	input llmrequest.ForwardingExecutionInput,
) (llmrequest.ForwardingExecutionResult, error) {
	if e == nil ||
		e.secrets == nil ||
		e.factory == nil ||
		e.maxResponseBodyBytes <= 0 {
		return llmrequest.ForwardingExecutionResult{},
			llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.ForwardingExecutionResult{}, fmt.Errorf(
			"%w: nil forwarding executor context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.ForwardingExecutionResult{}, err
	}

	route := input.Prepared.Plan.Route
	reseller := input.Prepared.Plan.Reseller
	if strings.TrimSpace(reseller.APIKeyEnv) == "" ||
		route.ResellerID != reseller.ID ||
		route.ProviderType != reseller.ProviderType ||
		input.Prepared.Payload == nil {
		return llmrequest.ForwardingExecutionResult{}, fmt.Errorf(
			"%w: invalid forwarding execution input",
			llmrequest.ErrStageContractViolation,
		)
	}

	secret, err := e.secrets.Resolve(ctx, reseller.APIKeyEnv)
	if err != nil {
		return llmrequest.ForwardingExecutionResult{}, fmt.Errorf(
			"resolve reseller secret: %w",
			err,
		)
	}
	if strings.TrimSpace(secret) == "" {
		return llmrequest.ForwardingExecutionResult{}, fmt.Errorf(
			"%w: resolved reseller secret is blank",
			llmrequest.ErrStageContractViolation,
		)
	}

	client, err := e.factory.Build(
		ports.ForwardingAdapterFactoryInput{
			Route:                route,
			Reseller:             reseller,
			ResellerAPIKey:       secret,
			MaxResponseBodyBytes: e.maxResponseBodyBytes,
		},
	)
	if err != nil {
		return llmrequest.ForwardingExecutionResult{}, fmt.Errorf(
			"build forwarding client: %w",
			err,
		)
	}

	response, err := client.Forward(
		ctx,
		ports.ForwardingClientRequest{
			Route: route,
			Body:  append([]byte(nil), input.Prepared.Payload...),
		},
	)
	if err != nil {
		return llmrequest.ForwardingExecutionResult{}, err
	}

	return llmrequest.ForwardingExecutionResult{
		Response: cloneExecutorForwardResponse(response),
	}, nil
}

func cloneExecutorForwardResponse(
	value ports.ForwardResponse,
) ports.ForwardResponse {
	headers := make(map[string][]string, len(value.Headers))
	for key, values := range value.Headers {
		headers[key] = append([]string(nil), values...)
	}
	if value.Headers == nil {
		headers = nil
	}
	return ports.ForwardResponse{
		StatusCode: value.StatusCode,
		Headers:    headers,
		Body:       append([]byte(nil), value.Body...),
	}
}
