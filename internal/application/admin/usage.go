package admin

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func usageState(r domain.UsageRecord) domain.AuditState {
	return domain.AuditState{
		"local_request_id": r.LocalRequestID, "user_id": r.UserID,
		"api_family": r.APIFamily, "endpoint_kind": r.EndpointKind,
		"client_model": r.ClientModel, "billing_model": r.BillingModel,
		"selected_reseller_id": r.SelectedResellerID, "selected_route_id": r.SelectedRouteID,
		"provider_type": r.ProviderType, "provider_model": r.ProviderModel,
		"usage": r.Usage, "usage_completeness": r.UsageCompleteness,
		"client_amount_cents":        r.ClientAmountCents,
		"charged_amount_cents":       r.ChargedAmountCents,
		"remaining_amount_cents":     r.RemainingAmountCents,
		"actual_upstream_cost_cents": r.ActualUpstreamCostCents,
		"currency":                   r.Currency, "status": r.Status,
		"failure_reason":            r.FailureReason,
		"billing_charge_request_id": r.BillingChargeRequestID,
		"created_at":                r.CreatedAt, "reserved_at": r.ReservedAt,
		"released_at": r.ReleasedAt, "billable_at": r.BillableAt,
		"charged_at": r.ChargedAt, "failed_at": r.FailedAt, "updated_at": r.UpdatedAt,
	}
}

func normalizeResolvedCompleteness(value string) string {
	switch value {
	case "detailed", "aggregate", "estimated":
		return value
	default:
		return "estimated"
	}
}

func (s *Service) ListUsageRecords(ctx context.Context, input UsageListInput) (ListResult[domain.UsageRecord], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[domain.UsageRecord]{}, err
	}
	if err := validateWindow(input.CreatedFrom, input.CreatedTo); err != nil {
		return ListResult[domain.UsageRecord]{}, err
	}
	if input.ProviderType != "" && !validateProviderType(input.ProviderType) {
		return ListResult[domain.UsageRecord]{}, ErrInvalidRequest
	}
	page, err := s.deps.Ledger.ListUsageRecords(ctx, ports.UsageListFilter{UserID: input.UserID, Status: input.Status, ProviderType: input.ProviderType, ClientModel: input.ClientModel, SelectedRouteID: input.SelectedRouteID, SelectedResellerID: input.SelectedResellerID, CreatedFrom: input.CreatedFrom, CreatedTo: input.CreatedTo, Page: pageReq})
	if err != nil {
		return ListResult[domain.UsageRecord]{}, mapStoreError(err)
	}
	return listResult(page, pageReq), nil
}

func (s *Service) GetUsageRecord(ctx context.Context, localRequestID string) (domain.UsageRecord, error) {
	if isBlank(localRequestID) {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	record, err := s.deps.Ledger.FindByLocalRequestID(ctx, localRequestID)
	if err != nil {
		return domain.UsageRecord{}, mapStoreError(err)
	}
	if record == nil {
		return domain.UsageRecord{}, ErrNotFound
	}
	return *record, nil
}

func (s *Service) loadPricingFailed(ctx context.Context, id string) (domain.UsageRecord, error) {
	if isBlank(id) {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	current, err := s.deps.Ledger.FindByLocalRequestID(ctx, id)
	if err != nil {
		return domain.UsageRecord{}, mapStoreError(err)
	}
	if current == nil {
		return domain.UsageRecord{}, ErrNotFound
	}
	if current.Status != domain.UsageStatusPricingFailed {
		return domain.UsageRecord{}, ErrStateConflict
	}
	if err := s.deps.UsagePolicy.ValidateRecord(*current); err != nil {
		return domain.UsageRecord{}, ErrStateConflict
	}
	return *current, nil
}

func (s *Service) persistPricingResolution(ctx context.Context, command CommandContext, current, next domain.UsageRecord, action domain.AuditAction, reason string) (domain.UsageRecord, error) {
	if err := s.deps.UsagePolicy.ValidateTransition(current.Status, next.Status); err != nil {
		return domain.UsageRecord{}, ErrStateConflict
	}
	if err := s.deps.UsagePolicy.ValidateRecord(next); err != nil {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	audit := auditContextWithReason(command, action, "usage_record", current.LocalRequestID, usageState(current), usageState(next), reason, next.UpdatedAt)
	result, err := s.deps.Ledger.ResolvePricingFailedWithAudit(ctx, current, next, audit)
	if err != nil {
		return domain.UsageRecord{}, mapStoreError(err)
	}
	if result.Applied {
		return next, nil
	}
	if result.Current == nil {
		return domain.UsageRecord{}, ErrStateConflict
	}
	return domain.UsageRecord{}, ErrStateConflict
}

func (s *Service) ResolveUsageBillable(ctx context.Context, command CommandContext, input ResolveBillableInput) (domain.UsageRecord, error) {
	if validateCommand(command) != nil || isBlank(input.Reason) || input.InputTokens < 0 || input.OutputTokens < 0 || input.ClientAmountCents < 0 || input.ActualUpstreamCostCents < 0 {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	current, err := s.loadPricingFailed(ctx, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	next := current
	next.Status = domain.UsageStatusBillable
	next.Usage.InputTokens = input.InputTokens
	next.Usage.OutputTokens = input.OutputTokens
	next.UsageCompleteness = normalizeResolvedCompleteness(next.UsageCompleteness)
	next.ClientAmountCents = input.ClientAmountCents
	next.ChargedAmountCents = 0
	next.RemainingAmountCents = input.ClientAmountCents
	next.ActualUpstreamCostCents = input.ActualUpstreamCostCents
	next.FailureReason = ""
	next.BillingChargeRequestID = ""
	next.BillableAt = &at
	next.ChargedAt = nil
	next.FailedAt = nil
	next.UpdatedAt = at
	return s.persistPricingResolution(ctx, command, current, next, domain.AuditActionUsageResolveBillable, input.Reason)
}

func (s *Service) ResolveUsageFailed(ctx context.Context, command CommandContext, input ResolveFailedInput) (domain.UsageRecord, error) {
	if validateCommand(command) != nil || isBlank(input.Reason) {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	current, err := s.loadPricingFailed(ctx, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	next := current
	next.Status = domain.UsageStatusFailed
	next.ClientAmountCents = 0
	next.ChargedAmountCents = 0
	next.RemainingAmountCents = 0
	next.FailureReason = "manual_resolution"
	next.BillingChargeRequestID = ""
	next.BillableAt = nil
	next.ChargedAt = nil
	next.FailedAt = &at
	next.UpdatedAt = at
	return s.persistPricingResolution(ctx, command, current, next, domain.AuditActionUsageResolveFailed, input.Reason)
}

func (s *Service) ResolveUsageCharged(ctx context.Context, command CommandContext, input ResolveChargedInput) (domain.UsageRecord, error) {
	if validateCommand(command) != nil || isBlank(input.Reason) || input.ChargedAmountCents < 0 || !validBillingChargeID(input.BillingChargeRequestID) {
		return domain.UsageRecord{}, ErrInvalidRequest
	}
	current, err := s.loadPricingFailed(ctx, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	next := current
	next.Status = domain.UsageStatusCharged
	next.UsageCompleteness = normalizeResolvedCompleteness(next.UsageCompleteness)
	next.ClientAmountCents = input.ChargedAmountCents
	next.ChargedAmountCents = input.ChargedAmountCents
	next.RemainingAmountCents = 0
	next.FailureReason = ""
	next.BillingChargeRequestID = input.BillingChargeRequestID
	next.BillableAt = &at
	next.ChargedAt = &at
	next.FailedAt = nil
	next.UpdatedAt = at
	return s.persistPricingResolution(ctx, command, current, next, domain.AuditActionUsageResolveCharged, input.Reason)
}
