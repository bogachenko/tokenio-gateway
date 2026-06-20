package llmrequest

import "context"

type AutoChargeServiceInput struct {
	UserID               string
	BillingSubjectUserID string
	Currency             string
}

type AutoChargeServiceResult struct {
	Deferred bool

	ProcessedBatchIDs  []string
	ChargedAmountCents int64

	BillingBalanceCents *int64
}

type llmRequestAutoChargeService interface {
	Run(
		context.Context,
		AutoChargeServiceInput,
	) (AutoChargeServiceResult, error)
}

type LLMRequestAutoCharger struct {
	service llmRequestAutoChargeService
}

var _ AutoCharger = (*LLMRequestAutoCharger)(nil)

func NewLLMRequestAutoCharger(
	service llmRequestAutoChargeService,
) (*LLMRequestAutoCharger, error) {
	if service == nil {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestAutoCharger{service: service}, nil
}

func (a *LLMRequestAutoCharger) Run(
	ctx context.Context,
	input AutoChargeInput,
) AutoChargeResult {
	if a == nil || a.service == nil {
		return AutoChargeResult{Status: AutoChargeStatusFailed}
	}

	result, err := a.service.Run(
		ctx,
		AutoChargeServiceInput{
			UserID:               input.Principal.UserID,
			BillingSubjectUserID: input.Principal.BillingSubjectUserID,
			Currency:             input.FinalUsageRecord.Currency,
		},
	)
	if err != nil {
		return AutoChargeResult{Status: AutoChargeStatusFailed}
	}

	status := AutoChargeStatusProcessed
	if result.Deferred && len(result.ProcessedBatchIDs) == 0 {
		status = AutoChargeStatusDeferred
	}

	var balance *int64
	if result.BillingBalanceCents != nil {
		copied := *result.BillingBalanceCents
		balance = &copied
	}

	return AutoChargeResult{
		Status:              status,
		ProcessedBatchIDs:   append([]string(nil), result.ProcessedBatchIDs...),
		ChargedAmountCents:  result.ChargedAmountCents,
		BillingBalanceCents: balance,
	}
}
