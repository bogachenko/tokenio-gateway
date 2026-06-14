package app

import (
	"context"
	"errors"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

type llmRequestAutoChargeService interface {
	Run(
		context.Context,
		billingapp.AutoChargeInput,
	) (billingapp.AutoChargeResult, error)
}

type LLMRequestAutoCharger struct {
	service llmRequestAutoChargeService
}

var _ llmrequest.AutoCharger = (*LLMRequestAutoCharger)(nil)

func NewLLMRequestAutoCharger(
	service llmRequestAutoChargeService,
) (*LLMRequestAutoCharger, error) {
	if service == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestAutoCharger{service: service}, nil
}

func (a *LLMRequestAutoCharger) Run(
	ctx context.Context,
	input llmrequest.AutoChargeInput,
) llmrequest.AutoChargeResult {
	if a == nil || a.service == nil {
		return llmrequest.AutoChargeResult{
			Status: llmrequest.AutoChargeStatusFailed,
		}
	}

	result, err := a.service.Run(
		ctx,
		billingapp.AutoChargeInput{
			UserID: input.Principal.UserID,
			BillingSubjectUserID: input.Principal.
				BillingSubjectUserID,
			Currency: input.FinalUsageRecord.Currency,
		},
	)
	switch {
	case errors.Is(err, billingapp.ErrChargeDeferred):
		return llmrequest.AutoChargeResult{
			Status: llmrequest.AutoChargeStatusDeferred,
		}
	case err != nil:
		return llmrequest.AutoChargeResult{
			Status: llmrequest.AutoChargeStatusFailed,
		}
	}

	status := llmrequest.AutoChargeStatusProcessed
	if result.Deferred &&
		len(result.ProcessedBatchIDs) == 0 {
		status = llmrequest.AutoChargeStatusDeferred
	}

	var balance *int64
	if result.BillingBalanceCents != nil {
		copied := *result.BillingBalanceCents
		balance = &copied
	}

	return llmrequest.AutoChargeResult{
		Status:              status,
		ProcessedBatchIDs:   append([]string(nil), result.ProcessedBatchIDs...),
		ChargedAmountCents:  result.ChargedAmountCents,
		BillingBalanceCents: balance,
	}
}
