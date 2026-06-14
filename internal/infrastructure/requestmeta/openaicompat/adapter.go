package openaicompat

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type Adapter struct{}

var (
	_ llmrequest.RequestParser      = (*Adapter)(nil)
	_ llmrequest.CapabilityDetector = (*Adapter)(nil)
)

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Parse(
	ctx context.Context,
	input llmrequest.ParseInput,
) (llmrequest.ParsedRequest, error) {
	if a == nil {
		return llmrequest.ParsedRequest{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			llmrequest.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	inspection, err := inspect(
		input.APIFamily,
		input.EndpointKind,
		input.Payload,
	)
	if err != nil {
		return llmrequest.ParsedRequest{}, err
	}
	return llmrequest.ParsedRequest{
		ClientModel: inspection.clientModel,
	}, nil
}

func (a *Adapter) Detect(
	ctx context.Context,
	input llmrequest.CapabilityInput,
) (domain.CapabilitySet, error) {
	if a == nil {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: nil OpenAI-compatible request metadata adapter",
			llmrequest.ErrStageContractViolation,
		)
	}
	if err := validateContext(ctx); err != nil {
		return domain.CapabilitySet{}, err
	}
	if strings.TrimSpace(input.ClientModel) == "" {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: blank parsed client model",
			llmrequest.ErrStageContractViolation,
		)
	}
	inspection, err := inspect(
		input.APIFamily,
		input.EndpointKind,
		input.Payload,
	)
	if err != nil {
		return domain.CapabilitySet{}, err
	}
	if inspection.clientModel != input.ClientModel {
		return domain.CapabilitySet{}, fmt.Errorf(
			"%w: parsed client model mismatch",
			llmrequest.ErrStageContractViolation,
		)
	}
	return inspection.capabilities, nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf(
			"%w: nil context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
