package admin

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func resellerState(r domain.Reseller) domain.AuditState {
	return domain.AuditState{
		"id": r.ID, "name": r.Name, "provider_type": r.ProviderType,
		"base_url": r.BaseURL, "api_key_env": r.APIKeyEnv, "enabled": r.Enabled,
		"balance_cents": r.BalanceCents, "reserved_cents": r.ReservedCents,
		"minimum_balance_cents": r.MinimumBalanceCents, "created_at": r.CreatedAt,
		"updated_at": r.UpdatedAt, "disabled_at": r.DisabledAt,
	}
}

func resellerView(r domain.Reseller, present bool) ResellerView {
	return ResellerView{ID: r.ID, Name: r.Name, ProviderType: r.ProviderType, BaseURL: r.BaseURL, APIKeyEnv: r.APIKeyEnv, APIKeyEnvPresent: present, Enabled: r.Enabled, BalanceCents: r.BalanceCents, ReservedCents: r.ReservedCents, MinimumBalanceCents: r.MinimumBalanceCents, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func validateReseller(r domain.Reseller) error {
	if isBlank(r.ID) || isBlank(r.Name) || !validateProviderType(r.ProviderType) || r.APIKeyEnv == "" {
		return ErrInvalidRequest
	}
	if err := domain.ValidateResellerBaseURL(r.BaseURL); err != nil {
		return ErrInvalidRequest
	}
	if r.ReservedCents < 0 || r.MinimumBalanceCents < 0 {
		return ErrInvalidRequest
	}
	if _, err := checkedAvailableResellerBalance(r); err != nil {
		return err
	}
	if requireUTC(r.CreatedAt) != nil || requireUTC(r.UpdatedAt) != nil || optionalUTC(r.DisabledAt) != nil {
		return ErrInvalidRequest
	}
	return nil
}

func (s *Service) ListResellers(ctx context.Context, input ResellerListInput) (ListResult[ResellerView], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[ResellerView]{}, err
	}
	if input.ProviderType != "" && !validateProviderType(input.ProviderType) {
		return ListResult[ResellerView]{}, ErrInvalidRequest
	}
	page, err := s.deps.Resellers.ListResellers(ctx, ports.ResellerListFilter{ProviderType: input.ProviderType, Enabled: input.Enabled, Page: pageReq})
	if err != nil {
		return ListResult[ResellerView]{}, mapStoreError(err)
	}
	items := make([]ResellerView, len(page.Items))
	for i, reseller := range page.Items {
		present, err := s.deps.Secrets.Exists(ctx, reseller.APIKeyEnv)
		if err != nil {
			return ListResult[ResellerView]{}, ErrStoreUnavailable
		}
		items[i] = resellerView(reseller, present)
	}
	return ListResult[ResellerView]{Data: items, Pagination: Pagination{Limit: pageReq.Limit, Offset: pageReq.Offset, Total: page.Total}}, nil
}

func (s *Service) GetReseller(ctx context.Context, id string) (ResellerView, error) {
	if isBlank(id) {
		return ResellerView{}, ErrInvalidRequest
	}
	reseller, err := s.deps.Resellers.FindResellerByID(ctx, id)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	if reseller == nil {
		return ResellerView{}, ErrNotFound
	}
	present, err := s.deps.Secrets.Exists(ctx, reseller.APIKeyEnv)
	if err != nil {
		return ResellerView{}, ErrStoreUnavailable
	}
	return resellerView(*reseller, present), nil
}

func (s *Service) CreateReseller(ctx context.Context, command CommandContext, input CreateResellerInput) (ResellerView, error) {
	if validateCommand(command) != nil {
		return ResellerView{}, ErrInvalidRequest
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return ResellerView{}, err
	}
	reseller := input.Reseller
	reseller.ReservedCents = 0
	reseller.CreatedAt = at
	reseller.UpdatedAt = at
	reseller.DisabledAt = nil
	if err := validateReseller(reseller); err != nil {
		return ResellerView{}, err
	}
	present, err := s.deps.Secrets.Exists(ctx, reseller.APIKeyEnv)
	if err != nil {
		return ResellerView{}, ErrStoreUnavailable
	}
	audit := auditContext(command, domain.AuditActionResellerCreate, "reseller", reseller.ID, nil, resellerState(reseller), at)
	created, err := s.deps.Resellers.CreateResellerWithAudit(ctx, reseller, audit)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	return resellerView(created, present), nil
}

func (s *Service) UpdateReseller(ctx context.Context, command CommandContext, input UpdateResellerInput) (ResellerView, error) {
	if validateCommand(command) != nil || isBlank(input.ID) || (input.Name == nil && input.BaseURL == nil && input.APIKeyEnv == nil && input.Enabled == nil && input.MinimumBalanceCents == nil) {
		return ResellerView{}, ErrInvalidRequest
	}
	current, err := s.deps.Resellers.FindResellerByID(ctx, input.ID)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	if current == nil {
		return ResellerView{}, ErrNotFound
	}
	next := *current
	if input.Name != nil {
		next.Name = *input.Name
	}
	if input.BaseURL != nil {
		next.BaseURL = *input.BaseURL
	}
	if input.APIKeyEnv != nil {
		next.APIKeyEnv = *input.APIKeyEnv
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if input.MinimumBalanceCents != nil {
		next.MinimumBalanceCents = *input.MinimumBalanceCents
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return ResellerView{}, err
	}
	next.UpdatedAt = at
	if current.Enabled && !next.Enabled {
		next.DisabledAt = &at
	}
	if !current.Enabled && next.Enabled {
		next.DisabledAt = nil
	}
	if err := validateReseller(next); err != nil {
		return ResellerView{}, err
	}
	present, err := s.deps.Secrets.Exists(ctx, next.APIKeyEnv)
	if err != nil {
		return ResellerView{}, ErrStoreUnavailable
	}
	audit := auditContext(command, domain.AuditActionResellerUpdate, "reseller", next.ID, resellerState(*current), resellerState(next), at)
	updated, err := s.deps.Resellers.CompareAndSwapResellerWithAudit(ctx, *current, next, audit)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	return resellerView(updated, present), nil
}

func (s *Service) SetResellerEnabled(ctx context.Context, command CommandContext, id string, enabled bool) (ResellerView, error) {
	if validateCommand(command) != nil || isBlank(id) {
		return ResellerView{}, ErrInvalidRequest
	}
	current, err := s.deps.Resellers.FindResellerByID(ctx, id)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	if current == nil {
		return ResellerView{}, ErrNotFound
	}
	if current.Enabled == enabled {
		return ResellerView{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return ResellerView{}, err
	}
	next := *current
	next.Enabled = enabled
	next.UpdatedAt = at
	action := domain.AuditActionResellerDisable
	if enabled {
		next.DisabledAt = nil
		action = domain.AuditActionResellerEnable
	} else {
		next.DisabledAt = &at
	}
	present, err := s.deps.Secrets.Exists(ctx, next.APIKeyEnv)
	if err != nil {
		return ResellerView{}, ErrStoreUnavailable
	}
	audit := auditContext(command, action, "reseller", id, resellerState(*current), resellerState(next), at)
	updated, err := s.deps.Resellers.CompareAndSwapResellerWithAudit(ctx, *current, next, audit)
	if err != nil {
		return ResellerView{}, mapStoreError(err)
	}
	return resellerView(updated, present), nil
}

func checkedAvailableResellerBalance(r domain.Reseller) (int64, error) {
	available, err := checkedSub(r.BalanceCents, r.ReservedCents)
	if err != nil {
		return 0, ErrInvalidRequest
	}
	available, err = checkedSub(available, r.MinimumBalanceCents)
	if err != nil {
		return 0, ErrInvalidRequest
	}
	return available, nil
}

func resellerBalance(r domain.Reseller) (ResellerBalance, error) {
	available, err := checkedAvailableResellerBalance(r)
	if err != nil {
		return ResellerBalance{}, err
	}
	return ResellerBalance{ResellerID: r.ID, BalanceCents: r.BalanceCents, ReservedCents: r.ReservedCents, MinimumBalanceCents: r.MinimumBalanceCents, AvailableBalanceCents: available, Currency: currencyRUB}, nil
}

func (s *Service) GetResellerBalance(ctx context.Context, id string) (ResellerBalance, error) {
	if isBlank(id) {
		return ResellerBalance{}, ErrInvalidRequest
	}
	reseller, err := s.deps.Resellers.FindResellerByID(ctx, id)
	if err != nil {
		return ResellerBalance{}, mapStoreError(err)
	}
	if reseller == nil {
		return ResellerBalance{}, ErrNotFound
	}
	return resellerBalance(*reseller)
}

func (s *Service) AdjustResellerBalance(ctx context.Context, command CommandContext, id string, delta int64, reason string) (ResellerBalance, error) {
	if validateCommand(command) != nil || isBlank(id) || isBlank(reason) {
		return ResellerBalance{}, ErrInvalidRequest
	}
	current, err := s.deps.Resellers.FindResellerByID(ctx, id)
	if err != nil {
		return ResellerBalance{}, mapStoreError(err)
	}
	if current == nil {
		return ResellerBalance{}, ErrNotFound
	}
	balance, err := checkedAdd(current.BalanceCents, delta)
	if err != nil {
		return ResellerBalance{}, err
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return ResellerBalance{}, err
	}
	next := *current
	next.BalanceCents = balance
	next.UpdatedAt = at
	result, err := resellerBalance(next)
	if err != nil {
		return ResellerBalance{}, err
	}
	audit := auditContextWithReason(command, domain.AuditActionResellerBalanceAdjust, "reseller", id, resellerState(*current), resellerState(next), reason, at)
	if _, err := s.deps.Resellers.CompareAndSwapResellerWithAudit(ctx, *current, next, audit); err != nil {
		return ResellerBalance{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) SetResellerBalance(ctx context.Context, command CommandContext, id string, balance int64, reason string) (ResellerBalance, error) {
	if validateCommand(command) != nil || isBlank(id) || isBlank(reason) {
		return ResellerBalance{}, ErrInvalidRequest
	}
	current, err := s.deps.Resellers.FindResellerByID(ctx, id)
	if err != nil {
		return ResellerBalance{}, mapStoreError(err)
	}
	if current == nil {
		return ResellerBalance{}, ErrNotFound
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return ResellerBalance{}, err
	}
	next := *current
	next.BalanceCents = balance
	next.UpdatedAt = at
	result, err := resellerBalance(next)
	if err != nil {
		return ResellerBalance{}, err
	}
	audit := auditContextWithReason(command, domain.AuditActionResellerBalanceSet, "reseller", id, resellerState(*current), resellerState(next), reason, at)
	if _, err := s.deps.Resellers.CompareAndSwapResellerWithAudit(ctx, *current, next, audit); err != nil {
		return ResellerBalance{}, mapStoreError(err)
	}
	return result, nil
}
