package app

import (
	"context"
	"fmt"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

type billingAdmissionService interface {
	Admit(
		context.Context,
		billingapp.AdmissionInput,
	) (billingapp.AdmissionResult, error)
}

type LLMRequestBillingAdmitter struct {
	billing billingAdmissionService
}

var _ llmrequest.BillingAdmitter = (*LLMRequestBillingAdmitter)(nil)

func NewLLMRequestBillingAdmitter(
	billing billingAdmissionService,
) (*LLMRequestBillingAdmitter, error) {
	if billing == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestBillingAdmitter{billing: billing}, nil
}

func (a *LLMRequestBillingAdmitter) Admit(
	ctx context.Context,
	input llmrequest.BillingAdmissionInput,
) (llmrequest.BillingAdmissionResult, error) {
	if a == nil || a.billing == nil {
		return llmrequest.BillingAdmissionResult{},
			llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.BillingAdmissionResult{}, fmt.Errorf(
			"%w: nil billing admission context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.BillingAdmissionResult{}, err
	}

	result, err := a.billing.Admit(
		ctx,
		billingapp.AdmissionInput{
			UserID: input.Principal.UserID,
			BillingSubjectUserID: input.Principal.
				BillingSubjectUserID,
			RequiredReserveCents: input.RequiredReserveCents,
			Currency:             input.Currency,
		},
	)
	if err != nil {
		return llmrequest.BillingAdmissionResult{}, fmt.Errorf(
			"admit LLM-request billing reserve: %w",
			err,
		)
	}

	return llmrequest.BillingAdmissionResult{
		Allowed:               result.Allowed,
		RemoteBalanceCents:    result.RemoteBalanceCents,
		PendingAmountCents:    result.PendingAmountCents,
		EffectiveBalanceCents: result.EffectiveBalanceCents,
		RequiredReserveCents:  result.RequiredReserveCents,
		Currency:              result.Currency,
	}, nil
}
