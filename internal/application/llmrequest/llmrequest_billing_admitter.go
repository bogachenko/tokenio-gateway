package llmrequest

import (
	"context"
	"fmt"
)

type billingAdmissionService interface {
	Admit(context.Context, BillingAdmissionInput) (BillingAdmissionResult, error)
}

type LLMRequestBillingAdmitter struct {
	billing billingAdmissionService
}

var _ BillingAdmitter = (*LLMRequestBillingAdmitter)(nil)

func NewLLMRequestBillingAdmitter(
	billing billingAdmissionService,
) (*LLMRequestBillingAdmitter, error) {
	if billing == nil {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestBillingAdmitter{billing: billing}, nil
}

func (a *LLMRequestBillingAdmitter) Admit(
	ctx context.Context,
	input BillingAdmissionInput,
) (BillingAdmissionResult, error) {
	if a == nil || a.billing == nil {
		return BillingAdmissionResult{}, ErrDependencyRequired
	}
	if ctx == nil {
		return BillingAdmissionResult{}, fmt.Errorf(
			"%w: nil billing admission context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return BillingAdmissionResult{}, err
	}

	result, err := a.billing.Admit(ctx, input)
	if err != nil {
		return BillingAdmissionResult{}, fmt.Errorf("admit billing: %w", err)
	}

	return result, nil
}
