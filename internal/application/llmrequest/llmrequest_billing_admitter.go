package llmrequest

import (
	"context"
	"fmt"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
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

	result, err := a.billing.Admit(
		ctx,
		billingapp.AdmissionInput{
			UserID:               input.Principal.UserID,
			BillingSubjectUserID: input.Principal.BillingSubjectUserID,
			RequiredReserveCents: input.RequiredReserveCents,
			Currency:             input.Currency,
		},
	)
	if err != nil {
		return BillingAdmissionResult{}, fmt.Errorf(
			"admit LLM-request billing reserve: %w",
			err,
		)
	}

	return BillingAdmissionResult{
		Allowed:               result.Allowed,
		RemoteBalanceCents:    result.RemoteBalanceCents,
		PendingAmountCents:    result.PendingAmountCents,
		EffectiveBalanceCents: result.EffectiveBalanceCents,
		RequiredReserveCents:  result.RequiredReserveCents,
		Currency:              result.Currency,
	}, nil
}
