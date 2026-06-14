package admin

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func routeState(r domain.Route) domain.AuditState {
	return domain.AuditState{
		"id": r.ID, "reseller_id": r.ResellerID, "provider_type": r.ProviderType,
		"api_family": r.APIFamily, "endpoint_kind": r.EndpointKind,
		"client_model": r.ClientModel, "provider_model": r.ProviderModel,
		"model_rewrite_policy": r.ModelRewritePolicy, "enabled": r.Enabled,
		"priority": r.Priority, "requests_per_minute": r.RequestsPerMinute,
		"tokens_per_minute": r.TokensPerMinute, "concurrent_requests": r.ConcurrentRequests,
		"default_max_output_tokens": r.DefaultMaxOutputTokens,
		"capabilities":              r.Capabilities, "cooldown_until": r.CooldownUntil,
		"cooldown_reason": r.CooldownReason, "last_error_code": r.LastErrorCode,
		"last_error_at": r.LastErrorAt, "created_at": r.CreatedAt,
		"updated_at": r.UpdatedAt, "disabled_at": r.DisabledAt,
	}
}

func priceState(p domain.RoutePrice) domain.AuditState {
	return domain.AuditState{
		"route_id": p.RouteID, "currency": p.Currency,
		"input_price_per_1m_tokens_cents":            p.InputPricePer1MTokensCents,
		"cached_input_price_per_1m_tokens_cents":     p.CachedInputPricePer1MTokensCents,
		"output_price_per_1m_tokens_cents":           p.OutputPricePer1MTokensCents,
		"reasoning_output_price_per_1m_tokens_cents": p.ReasoningOutputPricePer1MTokensCents,
		"image_input_price_per_1m_tokens_cents":      p.ImageInputPricePer1MTokensCents,
		"audio_input_price_per_1m_tokens_cents":      p.AudioInputPricePer1MTokensCents,
		"audio_output_price_per_1m_tokens_cents":     p.AudioOutputPricePer1MTokensCents,
		"file_input_price_per_1m_tokens_cents":       p.FileInputPricePer1MTokensCents,
		"video_input_price_per_1m_tokens_cents":      p.VideoInputPricePer1MTokensCents,
		"image_generation_price_per_unit_cents":      p.ImageGenerationPricePerUnitCents,
		"image_generation_unit_kind":                 p.ImageGenerationUnitKind,
		"markup_coefficient":                         p.MarkupCoefficient, "enabled": p.Enabled,
		"created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
	}
}

func (s *Service) ListRoutes(ctx context.Context, input RouteListInput) (ListResult[domain.Route], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[domain.Route]{}, err
	}
	if input.ProviderType != "" && !validateProviderType(input.ProviderType) {
		return ListResult[domain.Route]{}, ErrInvalidRequest
	}
	if input.APIFamily != "" && !validateAPIFamily(input.APIFamily) {
		return ListResult[domain.Route]{}, ErrInvalidRequest
	}
	if input.EndpointKind != "" && !validateEndpoint(input.EndpointKind) {
		return ListResult[domain.Route]{}, ErrInvalidRequest
	}
	page, err := s.deps.Routes.ListRoutes(ctx, ports.RouteListFilter{ResellerID: input.ResellerID, ProviderType: input.ProviderType, APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, ClientModel: input.ClientModel, Enabled: input.Enabled, Page: pageReq})
	if err != nil {
		return ListResult[domain.Route]{}, mapStoreError(err)
	}
	return listResult(page, pageReq), nil
}

func (s *Service) GetRoute(ctx context.Context, id string) (domain.Route, error) {
	if isBlank(id) {
		return domain.Route{}, ErrInvalidRequest
	}
	route, err := s.deps.Routes.FindRouteByID(ctx, id)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if route == nil {
		return domain.Route{}, ErrNotFound
	}
	return *route, nil
}

func (s *Service) CreateRoute(ctx context.Context, command CommandContext, route domain.Route) (domain.Route, error) {
	if validateCommand(command) != nil {
		return domain.Route{}, ErrInvalidRequest
	}
	reseller, err := s.deps.Resellers.FindResellerByID(ctx, route.ResellerID)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if reseller == nil {
		return domain.Route{}, ErrNotFound
	}
	if reseller.ProviderType != route.ProviderType ||
		!s.deps.AdapterSupport.SupportsForwardingAdapter(
			route.APIFamily,
			route.ProviderType,
		) {
		return domain.Route{}, ErrInvalidRequest
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.Route{}, err
	}
	route.CreatedAt = at
	route.UpdatedAt = at
	route.DisabledAt = nil
	if err := validateRoute(route); err != nil {
		return domain.Route{}, err
	}
	audit := auditContext(command, domain.AuditActionRouteCreate, "route", route.ID, nil, routeState(route), at)
	created, err := s.deps.Routes.CreateRouteWithAudit(ctx, route, audit)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	return created, nil
}

func (s *Service) UpdateRoute(ctx context.Context, command CommandContext, input UpdateRouteInput) (domain.Route, error) {
	if validateCommand(command) != nil || isBlank(input.ID) || (input.ProviderModel == nil && input.ModelRewritePolicy == nil && input.Enabled == nil && input.Priority == nil && input.RequestsPerMinute == nil && input.TokensPerMinute == nil && input.ConcurrentRequests == nil && input.DefaultMaxOutputTokens == nil && input.Capabilities == nil) {
		return domain.Route{}, ErrInvalidRequest
	}
	current, err := s.deps.Routes.FindRouteByID(ctx, input.ID)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if current == nil {
		return domain.Route{}, ErrNotFound
	}
	next := *current
	if input.ProviderModel != nil {
		next.ProviderModel = *input.ProviderModel
	}
	if input.ModelRewritePolicy != nil {
		next.ModelRewritePolicy = *input.ModelRewritePolicy
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if input.Priority != nil {
		next.Priority = *input.Priority
	}
	if input.RequestsPerMinute != nil {
		next.RequestsPerMinute = *input.RequestsPerMinute
	}
	if input.TokensPerMinute != nil {
		next.TokensPerMinute = *input.TokensPerMinute
	}
	if input.ConcurrentRequests != nil {
		next.ConcurrentRequests = *input.ConcurrentRequests
	}
	if input.DefaultMaxOutputTokens != nil {
		next.DefaultMaxOutputTokens = *input.DefaultMaxOutputTokens
	}
	if input.Capabilities != nil {
		next.Capabilities = *input.Capabilities
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.Route{}, err
	}
	next.UpdatedAt = at
	if current.Enabled && !next.Enabled {
		next.DisabledAt = &at
	}
	if !current.Enabled && next.Enabled {
		next.DisabledAt = nil
	}
	if err := validateRoute(next); err != nil {
		return domain.Route{}, err
	}
	reseller, err := s.deps.Resellers.FindResellerByID(ctx, next.ResellerID)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if reseller == nil {
		return domain.Route{}, ErrNotFound
	}
	if reseller.ProviderType != next.ProviderType ||
		(next.Enabled &&
			!s.deps.AdapterSupport.SupportsForwardingAdapter(
				next.APIFamily,
				next.ProviderType,
			)) {
		return domain.Route{}, ErrInvalidRequest
	}
	audit := auditContext(command, domain.AuditActionRouteUpdate, "route", next.ID, routeState(*current), routeState(next), at)
	updated, err := s.deps.Routes.CompareAndSwapRouteWithAudit(ctx, *current, next, audit)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	return updated, nil
}

func (s *Service) SetRouteEnabled(ctx context.Context, command CommandContext, id string, enabled bool) (domain.Route, error) {
	if validateCommand(command) != nil || isBlank(id) {
		return domain.Route{}, ErrInvalidRequest
	}
	current, err := s.deps.Routes.FindRouteByID(ctx, id)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if current == nil {
		return domain.Route{}, ErrNotFound
	}
	if current.Enabled == enabled {
		return domain.Route{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.Route{}, err
	}
	next := *current
	next.Enabled = enabled
	next.UpdatedAt = at
	action := domain.AuditActionRouteDisable
	if enabled {
		next.DisabledAt = nil
		action = domain.AuditActionRouteEnable
	} else {
		next.DisabledAt = &at
	}
	if err := validateRoute(next); err != nil {
		return domain.Route{}, err
	}
	if enabled &&
		!s.deps.AdapterSupport.SupportsForwardingAdapter(
			next.APIFamily,
			next.ProviderType,
		) {
		return domain.Route{}, ErrInvalidRequest
	}
	audit := auditContext(command, action, "route", id, routeState(*current), routeState(next), at)
	updated, err := s.deps.Routes.CompareAndSwapRouteWithAudit(ctx, *current, next, audit)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	return updated, nil
}

func (s *Service) GetRouteCooldown(ctx context.Context, id string) (domain.Route, error) {
	return s.GetRoute(ctx, id)
}

func (s *Service) SetRouteCooldown(ctx context.Context, command CommandContext, input SetCooldownInput) (domain.Route, error) {
	if validateCommand(command) != nil || isBlank(input.RouteID) || isBlank(input.CooldownReason) || requireUTC(input.CooldownUntil) != nil {
		return domain.Route{}, ErrInvalidRequest
	}
	current, err := s.deps.Routes.FindRouteByID(ctx, input.RouteID)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if current == nil {
		return domain.Route{}, ErrNotFound
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.Route{}, err
	}
	if !input.CooldownUntil.After(at) {
		return domain.Route{}, ErrInvalidRequest
	}
	next := *current
	next.CooldownUntil = &input.CooldownUntil
	next.CooldownReason = input.CooldownReason
	next.UpdatedAt = at
	if err := validateRoute(next); err != nil {
		return domain.Route{}, err
	}
	audit := auditContext(command, domain.AuditActionRouteCooldownSet, "route", next.ID, routeState(*current), routeState(next), at)
	updated, err := s.deps.Routes.CompareAndSwapRouteWithAudit(ctx, *current, next, audit)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	return updated, nil
}

func (s *Service) ClearRouteCooldown(ctx context.Context, command CommandContext, id string) (domain.Route, error) {
	if validateCommand(command) != nil || isBlank(id) {
		return domain.Route{}, ErrInvalidRequest
	}
	current, err := s.deps.Routes.FindRouteByID(ctx, id)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	if current == nil {
		return domain.Route{}, ErrNotFound
	}
	if current.CooldownUntil == nil && current.CooldownReason == "" {
		return domain.Route{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.Route{}, err
	}
	next := *current
	next.CooldownUntil = nil
	next.CooldownReason = ""
	next.UpdatedAt = at
	if err := validateRoute(next); err != nil {
		return domain.Route{}, err
	}
	audit := auditContext(command, domain.AuditActionRouteCooldownClear, "route", id, routeState(*current), routeState(next), at)
	updated, err := s.deps.Routes.CompareAndSwapRouteWithAudit(ctx, *current, next, audit)
	if err != nil {
		return domain.Route{}, mapStoreError(err)
	}
	return updated, nil
}

func validateRoutePriceForEndpoint(
	route domain.Route,
	price domain.RoutePrice,
) error {
	if route.ID == "" ||
		price.RouteID != route.ID {
		return ErrInvalidRequest
	}

	hasImageGenerationDimension :=
		price.ImageGenerationPricePerUnitCents != 0 ||
			price.ImageGenerationUnitKind !=
				domain.ImageGenerationUnitKindNone

	switch route.EndpointKind {
	case domain.EndpointChat:
		if hasImageGenerationDimension {
			return ErrInvalidRequest
		}
	case domain.EndpointEmbeddings:
		if price.CachedInputPricePer1MTokensCents != 0 ||
			price.OutputPricePer1MTokensCents != 0 ||
			price.ReasoningOutputPricePer1MTokensCents != 0 ||
			price.ImageInputPricePer1MTokensCents != 0 ||
			price.AudioInputPricePer1MTokensCents != 0 ||
			price.AudioOutputPricePer1MTokensCents != 0 ||
			price.FileInputPricePer1MTokensCents != 0 ||
			price.VideoInputPricePer1MTokensCents != 0 ||
			hasImageGenerationDimension {
			return ErrInvalidRequest
		}
	case domain.EndpointImagesGeneration:
		if price.InputPricePer1MTokensCents != 0 ||
			price.CachedInputPricePer1MTokensCents != 0 ||
			price.OutputPricePer1MTokensCents != 0 ||
			price.ReasoningOutputPricePer1MTokensCents != 0 ||
			price.ImageInputPricePer1MTokensCents != 0 ||
			price.AudioInputPricePer1MTokensCents != 0 ||
			price.AudioOutputPricePer1MTokensCents != 0 ||
			price.FileInputPricePer1MTokensCents != 0 ||
			price.VideoInputPricePer1MTokensCents != 0 ||
			price.ImageGenerationUnitKind !=
				domain.ImageGenerationUnitKindGeneratedImage {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}

	return nil
}

func (s *Service) GetRoutePrice(ctx context.Context, routeID string) (domain.RoutePrice, error) {
	if isBlank(routeID) {
		return domain.RoutePrice{}, ErrInvalidRequest
	}
	price, err := s.deps.Prices.FindRoutePrice(ctx, routeID)
	if err != nil {
		return domain.RoutePrice{}, mapStoreError(err)
	}
	if price == nil {
		return domain.RoutePrice{}, ErrNotFound
	}
	return *price, nil
}

func (s *Service) UpsertRoutePrice(ctx context.Context, command CommandContext, price domain.RoutePrice) (domain.RoutePrice, error) {
	if validateCommand(command) != nil || isBlank(price.RouteID) {
		return domain.RoutePrice{}, ErrInvalidRequest
	}
	route, err := s.deps.Routes.FindRouteByID(ctx, price.RouteID)
	if err != nil {
		return domain.RoutePrice{}, mapStoreError(err)
	}
	if route == nil {
		return domain.RoutePrice{}, ErrNotFound
	}
	current, err := s.deps.Prices.FindRoutePrice(ctx, price.RouteID)
	if err != nil && err != ports.ErrNotFound {
		return domain.RoutePrice{}, mapStoreError(err)
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.RoutePrice{}, err
	}
	if current == nil {
		price.CreatedAt = at
	} else {
		price.CreatedAt = current.CreatedAt
	}
	price.UpdatedAt = at
	if err := s.validatePrice(price); err != nil {
		return domain.RoutePrice{}, err
	}
	if err := validateRoutePriceForEndpoint(
		*route,
		price,
	); err != nil {
		return domain.RoutePrice{}, err
	}
	var before domain.AuditState
	if current != nil {
		before = priceState(*current)
	}
	audit := auditContext(command, domain.AuditActionRoutePriceUpsert, "route_price", price.RouteID, before, priceState(price), at)
	updated, err := s.deps.Prices.UpsertRoutePriceWithAudit(ctx, current, price, audit)
	if err != nil {
		return domain.RoutePrice{}, mapStoreError(err)
	}
	return updated, nil
}
