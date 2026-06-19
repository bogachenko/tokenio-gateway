package adminhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

const (
	basePath             = "/admin/v1"
	adminRequestIDHeader = "X-Admin-Request-ID"
)

type Authenticator interface{ Authenticate(string) (string, error) }

// Service is kept private so handlers can depend only on application behavior.
type Service interface {
	ListUsers(context.Context, application.UserListInput) (application.ListResult[domain.User], error)
	CreateUser(context.Context, application.CommandContext, application.CreateUserInput) (domain.User, error)
	SetUserEnabled(context.Context, application.CommandContext, string, bool) (domain.User, error)
	ListAPIKeys(context.Context, string, int, int) (application.ListResult[application.APIKeyView], error)
	ListAPIKeyProvisionings(
		context.Context,
		application.APIKeyProvisioningListInput,
	) (application.ListResult[application.APIKeyProvisioningView], error)
	GetAPIKeyProvisioning(context.Context, string) (application.APIKeyProvisioningView, error)
	ListRouteEvents(context.Context, application.RouteEventListInput) (application.ListResult[domain.RouteEvent], error)
	ListTelegramAlerts(context.Context, application.TelegramAlertListInput) (application.ListResult[application.TelegramAlertView], error)
	RetryTelegramAlert(context.Context, application.CommandContext, string, string) (application.TelegramAlertView, error)
	CreateAPIKey(context.Context, application.CommandContext, application.CreateAPIKeyInput) (application.CreatedAPIKey, error)
	RevokeAPIKey(context.Context, application.CommandContext, string) (application.APIKeyView, error)
	ListResellers(context.Context, application.ResellerListInput) (application.ListResult[application.ResellerView], error)
	CreateReseller(context.Context, application.CommandContext, application.CreateResellerInput) (application.ResellerView, error)
	UpdateReseller(context.Context, application.CommandContext, application.UpdateResellerInput) (application.ResellerView, error)
	SetResellerEnabled(context.Context, application.CommandContext, string, bool) (application.ResellerView, error)
	GetResellerBalance(context.Context, string) (application.ResellerBalance, error)
	AdjustResellerBalance(context.Context, application.CommandContext, string, int64, string) (application.ResellerBalance, error)
	SetResellerBalance(context.Context, application.CommandContext, string, int64, string) (application.ResellerBalance, error)
	ListRoutes(context.Context, application.RouteListInput) (application.ListResult[domain.Route], error)
	CreateRoute(context.Context, application.CommandContext, domain.Route) (domain.Route, error)
	UpdateRoute(context.Context, application.CommandContext, application.UpdateRouteInput) (domain.Route, error)
	SetRouteEnabled(context.Context, application.CommandContext, string, bool) (domain.Route, error)
	GetRouteCooldown(context.Context, string) (domain.Route, error)
	SetRouteCooldown(context.Context, application.CommandContext, application.SetCooldownInput) (domain.Route, error)
	ClearRouteCooldown(context.Context, application.CommandContext, string) (domain.Route, error)
	GetRoutePrice(context.Context, string) (domain.RoutePrice, error)
	UpsertRoutePrice(context.Context, application.CommandContext, domain.RoutePrice) (domain.RoutePrice, error)
	ListUsageRecords(context.Context, application.UsageListInput) (application.ListResult[domain.UsageRecord], error)
	GetUsageRecord(context.Context, string) (domain.UsageRecord, error)
	ResolveUsageBillable(context.Context, application.CommandContext, application.ResolveBillableInput) (domain.UsageRecord, error)
	ResolveUsageFailed(context.Context, application.CommandContext, application.ResolveFailedInput) (domain.UsageRecord, error)
	ResolveUsageCharged(context.Context, application.CommandContext, application.ResolveChargedInput) (domain.UsageRecord, error)
	ListBillingChargeBatches(context.Context, application.BillingBatchListInput) (application.ListResult[domain.BillingChargeBatch], error)
	GetBillingChargeBatch(context.Context, string) (ports.BillingChargeBatchSnapshot, error)
	RetryFailedBillingChargeBatch(context.Context, application.CommandContext, string) (domain.BillingChargeBatch, error)
	ListAuditEntries(context.Context, application.AuditListInput) (application.ListResult[domain.AdminAuditEntry], error)
}

type Router struct {
	service Service
	auth    Authenticator
	ids     ports.RequestIDGenerator
}

func NewRouter(service Service, authenticator Authenticator, ids ports.RequestIDGenerator) (*Router, error) {
	if service == nil || authenticator == nil || ids == nil {
		return nil, errors.New("admin router dependency is required")
	}
	return &Router{service: service, auth: authenticator, ids: ids}, nil
}

func (h *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != basePath && !strings.HasPrefix(r.URL.Path, basePath+"/") {
		writeError(w, "", http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
		return
	}
	requestID, err := h.ids.NewAdminRequestID()
	if err != nil || !validAdminRequestID(requestID) {
		writeError(w, "", http.StatusInternalServerError, domain.ErrorCodeInternalError, "Internal error")
		return
	}
	w.Header().Set(adminRequestIDHeader, requestID)
	subject, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, auth.ErrAdminAuthorizationRequired) {
			writeError(w, requestID, http.StatusUnauthorized, domain.ErrorCodeAdminUnauthorized, "Admin authorization is required")
			return
		}
		writeError(w, requestID, http.StatusForbidden, domain.ErrorCodeAdminForbidden, "Admin access denied")
		return
	}
	command := application.CommandContext{RequestID: requestID, AdminSubject: subject}
	h.dispatch(w, r, command)
}

func (h *Router) dispatch(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	path := strings.TrimPrefix(r.URL.Path, basePath)
	segments := splitPath(path)
	if len(segments) == 1 {
		switch segments[0] {
		case "users":
			h.handleUsers(w, r, command)
			return
		case "resellers":
			h.handleResellers(w, r, command)
			return
		case "routes":
			h.handleRoutes(w, r, command)
			return
		case "usage-records":
			h.handleUsageRecords(w, r, command)
			return
		case "api-key-provisionings":
			h.handleAPIKeyProvisionings(w, r, command)
			return
		case "route-events":
			h.handleRouteEvents(w, r, command)
			return
		case "telegram-alerts":
			h.handleTelegramAlerts(w, r, command)
			return
		case "billing-charge-batches":
			h.handleBillingBatches(w, r, command)
			return
		case "audit-log":
			h.handleAuditLog(w, r)
			return
		}
	}
	if len(segments) >= 2 {
		switch segments[0] {
		case "users":
			h.handleUserPath(w, r, command, segments[1:])
			return
		case "api-keys":
			h.handleAPIKeyPath(w, r, command, segments[1:])
			return
		case "resellers":
			h.handleResellerPath(w, r, command, segments[1:])
			return
		case "routes":
			h.handleRoutePath(w, r, command, segments[1:])
			return
		case "usage-records":
			h.handleUsagePath(w, r, command, segments[1:])
			return
		case "billing-charge-batches":
			h.handleBillingBatchPath(w, r, command, segments[1:])
			return
		case "telegram-alerts":
			h.handleTelegramAlertPath(w, r, command, segments[1:])
			return
		case "api-key-provisionings":
			h.handleAPIKeyProvisioningPath(w, r, command, segments[1:])
			return
		}
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" {
			return nil
		}
	}
	return parts
}

func validAdminRequestID(id string) bool {
	return strings.HasPrefix(id, "admreq_") && len(id) > len("admreq_")
}

func (h *Router) handleUsers(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	switch r.Method {
	case http.MethodGet:
		page, ok := parsePage(w, r, command.RequestID)
		if !ok {
			return
		}
		enabled, ok := parseOptionalBool(w, r, command.RequestID, "enabled")
		if !ok {
			return
		}
		result, err := h.service.ListUsers(r.Context(), application.UserListInput{Enabled: enabled, Email: r.URL.Query().Get("email"), Limit: page.limit, Offset: page.offset})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeList(w, result.Data, result.Pagination)
	case http.MethodPost:
		var dto createUserDTO
		if !decodeJSON(w, r, command.RequestID, &dto) {
			return
		}
		created, err := h.service.CreateUser(r.Context(), command, application.CreateUserInput{ID: dto.ID, ExternalBillingUserID: dto.ExternalBillingUserID, Email: dto.Email, Name: dto.Name})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, created)
	default:
		methodNotAllowed(w, command.RequestID, "GET, POST")
	}
}

func (h *Router) handleUserPath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	if len(parts) == 2 && (parts[1] == "enable" || parts[1] == "disable") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		updated, err := h.service.SetUserEnabled(r.Context(), command, parts[0], parts[1] == "enable")
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, updated)
		return
	}
	if len(parts) == 2 && parts[1] == "api-keys" {
		switch r.Method {
		case http.MethodGet:
			page, ok := parsePage(w, r, command.RequestID)
			if !ok {
				return
			}
			result, err := h.service.ListAPIKeys(r.Context(), parts[0], page.limit, page.offset)
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeList(w, result.Data, result.Pagination)
		case http.MethodPost:
			var dto createAPIKeyDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			created, err := h.service.CreateAPIKey(r.Context(), command, application.CreateAPIKeyInput{UserID: parts[0], Name: dto.Name, ExpiresAt: dto.ExpiresAt})
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, created)
		default:
			methodNotAllowed(w, command.RequestID, "GET, POST")
		}
		return
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func (h *Router) handleAPIKeyPath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	if len(parts) != 2 || parts[1] != "revoke" {
		writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, command.RequestID, "POST")
		return
	}
	updated, err := h.service.RevokeAPIKey(r.Context(), command, parts[0])
	if err != nil {
		writeApplicationError(w, command.RequestID, err)
		return
	}
	writeData(w, updated)
}

func (h *Router) handleResellers(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	switch r.Method {
	case http.MethodGet:
		page, ok := parsePage(w, r, command.RequestID)
		if !ok {
			return
		}
		enabled, ok := parseOptionalBool(w, r, command.RequestID, "enabled")
		if !ok {
			return
		}
		result, err := h.service.ListResellers(r.Context(), application.ResellerListInput{ProviderType: domain.ProviderType(r.URL.Query().Get("provider_type")), Enabled: enabled, Limit: page.limit, Offset: page.offset})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeList(w, result.Data, result.Pagination)
	case http.MethodPost:
		var dto createResellerDTO
		if !decodeJSON(w, r, command.RequestID, &dto) {
			return
		}
		reseller := domain.Reseller{ID: dto.ID, Name: dto.Name, ProviderType: dto.ProviderType, BaseURL: dto.BaseURL, APIKeyEnv: dto.APIKeyEnv, Enabled: dto.Enabled, BalanceCents: dto.BalanceCents, MinimumBalanceCents: dto.MinimumBalanceCents}
		created, err := h.service.CreateReseller(r.Context(), command, application.CreateResellerInput{Reseller: reseller})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, created)
	default:
		methodNotAllowed(w, command.RequestID, "GET, POST")
	}
}

func (h *Router) handleResellerPath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	id := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodPatch {
			methodNotAllowed(w, command.RequestID, "PATCH")
			return
		}
		var dto updateResellerDTO
		if !decodeJSON(w, r, command.RequestID, &dto) {
			return
		}
		updated, err := h.service.UpdateReseller(r.Context(), command, application.UpdateResellerInput{ID: id, Name: dto.Name, BaseURL: dto.BaseURL, APIKeyEnv: dto.APIKeyEnv, Enabled: dto.Enabled, MinimumBalanceCents: dto.MinimumBalanceCents})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, updated)
		return
	}
	if len(parts) == 2 && (parts[1] == "enable" || parts[1] == "disable") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		updated, err := h.service.SetResellerEnabled(r.Context(), command, id, parts[1] == "enable")
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, updated)
		return
	}
	if len(parts) == 2 && parts[1] == "balance" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, command.RequestID, "GET")
			return
		}
		balance, err := h.service.GetResellerBalance(r.Context(), id)
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, balance)
		return
	}
	if len(parts) == 3 && parts[1] == "balance" && (parts[2] == "adjust" || parts[2] == "set") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		var result application.ResellerBalance
		var err error
		if parts[2] == "adjust" {
			var dto adjustBalanceDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			result, err = h.service.AdjustResellerBalance(r.Context(), command, id, dto.DeltaCents, dto.Reason)
		} else {
			var dto setBalanceDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			result, err = h.service.SetResellerBalance(r.Context(), command, id, dto.BalanceCents, dto.Reason)
		}
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, result)
		return
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func (h *Router) handleRoutes(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	switch r.Method {
	case http.MethodGet:
		page, ok := parsePage(w, r, command.RequestID)
		if !ok {
			return
		}
		enabled, ok := parseOptionalBool(w, r, command.RequestID, "enabled")
		if !ok {
			return
		}
		result, err := h.service.ListRoutes(r.Context(), application.RouteListInput{ResellerID: r.URL.Query().Get("reseller_id"), ProviderType: domain.ProviderType(r.URL.Query().Get("provider_type")), APIFamily: domain.APIFamily(r.URL.Query().Get("api_family")), EndpointKind: domain.EndpointKind(r.URL.Query().Get("endpoint_kind")), ClientModel: r.URL.Query().Get("client_model"), Enabled: enabled, Limit: page.limit, Offset: page.offset})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeList(w, result.Data, result.Pagination)
	case http.MethodPost:
		var dto createRouteDTO
		if !decodeJSON(w, r, command.RequestID, &dto) {
			return
		}
		if dto.Capabilities == nil {
			writeAdminValidationError(w, command.RequestID)
			return
		}
		route := domain.Route{ID: dto.ID, ResellerID: dto.ResellerID, ProviderType: dto.ProviderType, APIFamily: dto.APIFamily, EndpointKind: dto.EndpointKind, ClientModel: dto.ClientModel, ProviderModel: dto.ProviderModel, ModelRewritePolicy: dto.ModelRewritePolicy, Enabled: dto.Enabled, Priority: dto.Priority, RequestsPerMinute: dto.RequestsPerMinute, TokensPerMinute: dto.TokensPerMinute, ConcurrentRequests: dto.ConcurrentRequests, DefaultMaxOutputTokens: dto.DefaultMaxOutputTokens, Capabilities: *dto.Capabilities}
		created, err := h.service.CreateRoute(r.Context(), command, route)
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, created)
	default:
		methodNotAllowed(w, command.RequestID, "GET, POST")
	}
}

func (h *Router) handleRoutePath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	id := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodPatch {
			methodNotAllowed(w, command.RequestID, "PATCH")
			return
		}
		var dto updateRouteDTO
		if !decodeJSON(w, r, command.RequestID, &dto) {
			return
		}
		updated, err := h.service.UpdateRoute(r.Context(), command, application.UpdateRouteInput{ID: id, ProviderModel: dto.ProviderModel, ModelRewritePolicy: dto.ModelRewritePolicy, Enabled: dto.Enabled, Priority: dto.Priority, RequestsPerMinute: dto.RequestsPerMinute, TokensPerMinute: dto.TokensPerMinute, ConcurrentRequests: dto.ConcurrentRequests, DefaultMaxOutputTokens: dto.DefaultMaxOutputTokens, Capabilities: dto.Capabilities})
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, updated)
		return
	}
	if len(parts) == 2 && (parts[1] == "enable" || parts[1] == "disable") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		updated, err := h.service.SetRouteEnabled(r.Context(), command, id, parts[1] == "enable")
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, updated)
		return
	}
	if len(parts) == 2 && parts[1] == "cooldown" {
		switch r.Method {
		case http.MethodGet:
			route, err := h.service.GetRouteCooldown(r.Context(), id)
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, cooldownDTO{RouteID: route.ID, CooldownUntil: route.CooldownUntil, CooldownReason: route.CooldownReason, LastErrorCode: route.LastErrorCode, LastErrorAt: route.LastErrorAt})
		case http.MethodPost:
			var dto setCooldownDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			route, err := h.service.SetRouteCooldown(r.Context(), command, application.SetCooldownInput{RouteID: id, CooldownUntil: dto.CooldownUntil, CooldownReason: dto.CooldownReason})
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, cooldownDTO{RouteID: route.ID, CooldownUntil: route.CooldownUntil, CooldownReason: route.CooldownReason, LastErrorCode: route.LastErrorCode, LastErrorAt: route.LastErrorAt})
		case http.MethodDelete:
			route, err := h.service.ClearRouteCooldown(r.Context(), command, id)
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, cooldownDTO{RouteID: route.ID, CooldownUntil: route.CooldownUntil, CooldownReason: route.CooldownReason, LastErrorCode: route.LastErrorCode, LastErrorAt: route.LastErrorAt})
		default:
			methodNotAllowed(w, command.RequestID, "GET, POST, DELETE")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "price" {
		switch r.Method {
		case http.MethodGet:
			price, err := h.service.GetRoutePrice(r.Context(), id)
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, price)
		case http.MethodPut:
			var dto routePriceDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			price := domain.RoutePrice{RouteID: id, Currency: dto.Currency, InputPricePer1MTokensCents: dto.InputPricePer1MTokensCents, CachedInputPricePer1MTokensCents: dto.CachedInputPricePer1MTokensCents, OutputPricePer1MTokensCents: dto.OutputPricePer1MTokensCents, ReasoningOutputPricePer1MTokensCents: dto.ReasoningOutputPricePer1MTokensCents, ImageInputPricePer1MTokensCents: dto.ImageInputPricePer1MTokensCents, AudioInputPricePer1MTokensCents: dto.AudioInputPricePer1MTokensCents, AudioOutputPricePer1MTokensCents: dto.AudioOutputPricePer1MTokensCents, FileInputPricePer1MTokensCents: dto.FileInputPricePer1MTokensCents, VideoInputPricePer1MTokensCents: dto.VideoInputPricePer1MTokensCents, ImageGenerationPricePerUnitCents: dto.ImageGenerationPricePerUnitCents, ImageGenerationUnitKind: dto.ImageGenerationUnitKind, MarkupCoefficient: dto.MarkupCoefficient, Enabled: dto.Enabled}
			updated, err := h.service.UpsertRoutePrice(r.Context(), command, price)
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, updated)
		default:
			methodNotAllowed(w, command.RequestID, "GET, PUT")
		}
		return
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func (h *Router) handleUsageRecords(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, command.RequestID, "GET")
		return
	}
	page, ok := parsePage(w, r, command.RequestID)
	if !ok {
		return
	}
	from, ok := parseOptionalTime(w, r, command.RequestID, "created_from")
	if !ok {
		return
	}
	to, ok := parseOptionalTime(w, r, command.RequestID, "created_to")
	if !ok {
		return
	}
	result, err := h.service.ListUsageRecords(r.Context(), application.UsageListInput{UserID: r.URL.Query().Get("user_id"), Status: domain.UsageStatus(r.URL.Query().Get("status")), ProviderType: domain.ProviderType(r.URL.Query().Get("provider_type")), ClientModel: r.URL.Query().Get("client_model"), SelectedRouteID: r.URL.Query().Get("selected_route_id"), SelectedResellerID: r.URL.Query().Get("selected_reseller_id"), CreatedFrom: from, CreatedTo: to, Limit: page.limit, Offset: page.offset})
	if err != nil {
		writeApplicationError(w, command.RequestID, err)
		return
	}
	writeList(w, result.Data, result.Pagination)
}

func (h *Router) handleUsagePath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	id := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, command.RequestID, "GET")
			return
		}
		record, err := h.service.GetUsageRecord(r.Context(), id)
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, record)
		return
	}
	if len(parts) == 3 && parts[1] == "resolve" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		switch parts[2] {
		case "billable":
			var dto resolveBillableDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			record, err := h.service.ResolveUsageBillable(r.Context(), command, application.ResolveBillableInput{LocalRequestID: id, InputTokens: dto.InputTokens, OutputTokens: dto.OutputTokens, ClientAmountCents: dto.ClientAmountCents, ActualUpstreamCostCents: dto.ActualUpstreamCostCents, Reason: dto.Reason})
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, record)
		case "failed":
			var dto reasonDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			record, err := h.service.ResolveUsageFailed(r.Context(), command, application.ResolveFailedInput{LocalRequestID: id, Reason: dto.Reason})
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, record)
		case "charged":
			var dto resolveChargedDTO
			if !decodeJSON(w, r, command.RequestID, &dto) {
				return
			}
			record, err := h.service.ResolveUsageCharged(r.Context(), command, application.ResolveChargedInput{LocalRequestID: id, ChargedAmountCents: dto.ChargedAmountCents, BillingChargeRequestID: dto.BillingChargeRequestID, Reason: dto.Reason})
			if err != nil {
				writeApplicationError(w, command.RequestID, err)
				return
			}
			writeData(w, record)
		default:
			writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
		}
		return
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func (h *Router) handleBillingBatches(w http.ResponseWriter, r *http.Request, command application.CommandContext) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, command.RequestID, "GET")
		return
	}
	page, ok := parsePage(w, r, command.RequestID)
	if !ok {
		return
	}
	from, ok := parseOptionalTime(w, r, command.RequestID, "created_from")
	if !ok {
		return
	}
	to, ok := parseOptionalTime(w, r, command.RequestID, "created_to")
	if !ok {
		return
	}
	result, err := h.service.ListBillingChargeBatches(r.Context(), application.BillingBatchListInput{UserID: r.URL.Query().Get("user_id"), ProviderType: domain.ProviderType(r.URL.Query().Get("provider_type")), ClientModel: r.URL.Query().Get("client_model"), Status: domain.BillingChargeStatus(r.URL.Query().Get("billing_status")), CreatedFrom: from, CreatedTo: to, Limit: page.limit, Offset: page.offset})
	if err != nil {
		writeApplicationError(w, command.RequestID, err)
		return
	}
	writeList(w, result.Data, result.Pagination)
}

func (h *Router) handleBillingBatchPath(w http.ResponseWriter, r *http.Request, command application.CommandContext, parts []string) {
	id := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, command.RequestID, "GET")
			return
		}
		snapshot, err := h.service.GetBillingChargeBatch(r.Context(), id)
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, snapshot)
		return
	}
	if len(parts) == 2 && parts[1] == "retry" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, command.RequestID, "POST")
			return
		}
		batch, err := h.service.RetryFailedBillingChargeBatch(r.Context(), command, id)
		if err != nil {
			writeApplicationError(w, command.RequestID, err)
			return
		}
		writeData(w, batch)
		return
	}
	writeError(w, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
}

func (h *Router) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	requestID := w.Header().Get(adminRequestIDHeader)
	if r.Method != http.MethodGet {
		methodNotAllowed(w, requestID, "GET")
		return
	}
	page, ok := parsePage(w, r, requestID)
	if !ok {
		return
	}
	from, ok := parseOptionalTime(w, r, requestID, "created_from")
	if !ok {
		return
	}
	to, ok := parseOptionalTime(w, r, requestID, "created_to")
	if !ok {
		return
	}
	result, err := h.service.ListAuditEntries(r.Context(), application.AuditListInput{AdminSubject: r.URL.Query().Get("admin_subject"), Action: domain.AuditAction(r.URL.Query().Get("action")), EntityType: r.URL.Query().Get("entity_type"), EntityID: r.URL.Query().Get("entity_id"), CreatedFrom: from, CreatedTo: to, Limit: page.limit, Offset: page.offset})
	if err != nil {
		writeApplicationError(w, requestID, err)
		return
	}
	writeList(w, result.Data, result.Pagination)
}

// DTOs intentionally use pointers for PATCH fields.
type createUserDTO struct {
	ID                    string `json:"id"`
	ExternalBillingUserID string `json:"external_billing_user_id"`
	Email                 string `json:"email"`
	Name                  string `json:"name"`
}
type createAPIKeyDTO struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type createResellerDTO struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	ProviderType        domain.ProviderType `json:"provider_type"`
	BaseURL             string              `json:"base_url"`
	APIKeyEnv           string              `json:"api_key_env"`
	Enabled             bool                `json:"enabled"`
	BalanceCents        int64               `json:"balance_cents"`
	MinimumBalanceCents int64               `json:"minimum_balance_cents"`
}
type updateResellerDTO struct {
	Name                *string `json:"name"`
	BaseURL             *string `json:"base_url"`
	APIKeyEnv           *string `json:"api_key_env"`
	Enabled             *bool   `json:"enabled"`
	MinimumBalanceCents *int64  `json:"minimum_balance_cents"`
}
type adjustBalanceDTO struct {
	DeltaCents int64  `json:"delta_cents"`
	Reason     string `json:"reason"`
}

type setBalanceDTO struct {
	BalanceCents int64  `json:"balance_cents"`
	Reason       string `json:"reason"`
}

type createRouteDTO struct {
	ID                     string                    `json:"id"`
	ResellerID             string                    `json:"reseller_id"`
	ProviderType           domain.ProviderType       `json:"provider_type"`
	APIFamily              domain.APIFamily          `json:"api_family"`
	EndpointKind           domain.EndpointKind       `json:"endpoint_kind"`
	ClientModel            string                    `json:"client_model"`
	ProviderModel          string                    `json:"provider_model"`
	ModelRewritePolicy     domain.ModelRewritePolicy `json:"model_rewrite_policy"`
	Enabled                bool                      `json:"enabled"`
	Priority               int                       `json:"priority"`
	RequestsPerMinute      int                       `json:"requests_per_minute"`
	TokensPerMinute        int                       `json:"tokens_per_minute"`
	ConcurrentRequests     int                       `json:"concurrent_requests"`
	DefaultMaxOutputTokens int64                     `json:"default_max_output_tokens"`
	Capabilities           *domain.CapabilitySet     `json:"capabilities"`
}
type routePriceDTO struct {
	Currency                             string                         `json:"currency"`
	InputPricePer1MTokensCents           int64                          `json:"input_price_per_1m_tokens_cents"`
	CachedInputPricePer1MTokensCents     int64                          `json:"cached_input_price_per_1m_tokens_cents"`
	OutputPricePer1MTokensCents          int64                          `json:"output_price_per_1m_tokens_cents"`
	ReasoningOutputPricePer1MTokensCents int64                          `json:"reasoning_output_price_per_1m_tokens_cents"`
	ImageInputPricePer1MTokensCents      int64                          `json:"image_input_price_per_1m_tokens_cents"`
	AudioInputPricePer1MTokensCents      int64                          `json:"audio_input_price_per_1m_tokens_cents"`
	AudioOutputPricePer1MTokensCents     int64                          `json:"audio_output_price_per_1m_tokens_cents"`
	FileInputPricePer1MTokensCents       int64                          `json:"file_input_price_per_1m_tokens_cents"`
	VideoInputPricePer1MTokensCents      int64                          `json:"video_input_price_per_1m_tokens_cents"`
	ImageGenerationPricePerUnitCents     int64                          `json:"image_generation_price_per_unit_cents"`
	ImageGenerationUnitKind              domain.ImageGenerationUnitKind `json:"image_generation_unit_kind"`
	MarkupCoefficient                    float64                        `json:"markup_coefficient"`
	Enabled                              bool                           `json:"enabled"`
}

type updateRouteDTO struct {
	ProviderModel          *string                    `json:"provider_model"`
	ModelRewritePolicy     *domain.ModelRewritePolicy `json:"model_rewrite_policy"`
	Enabled                *bool                      `json:"enabled"`
	Priority               *int                       `json:"priority"`
	RequestsPerMinute      *int                       `json:"requests_per_minute"`
	TokensPerMinute        *int                       `json:"tokens_per_minute"`
	ConcurrentRequests     *int                       `json:"concurrent_requests"`
	DefaultMaxOutputTokens *int64                     `json:"default_max_output_tokens"`
	Capabilities           *domain.CapabilitySet      `json:"capabilities"`
}
type setCooldownDTO struct {
	CooldownUntil  time.Time `json:"cooldown_until"`
	CooldownReason string    `json:"cooldown_reason"`
}
type cooldownDTO struct {
	RouteID        string     `json:"route_id"`
	CooldownUntil  *time.Time `json:"cooldown_until"`
	CooldownReason string     `json:"cooldown_reason"`
	LastErrorCode  string     `json:"last_error_code"`
	LastErrorAt    *time.Time `json:"last_error_at"`
}
type resolveBillableDTO struct {
	InputTokens             int64  `json:"input_tokens"`
	OutputTokens            int64  `json:"output_tokens"`
	ClientAmountCents       int64  `json:"client_amount_cents"`
	ActualUpstreamCostCents int64  `json:"actual_upstream_cost_cents"`
	Reason                  string `json:"reason"`
}
type reasonDTO struct {
	Reason string `json:"reason"`
}
type resolveChargedDTO struct {
	ChargedAmountCents     int64  `json:"charged_amount_cents"`
	BillingChargeRequestID string `json:"billing_charge_request_id"`
	Reason                 string `json:"reason"`
}

type pageQuery struct{ limit, offset int }

func parsePage(w http.ResponseWriter, r *http.Request, requestID string) (pageQuery, bool) {
	limit, ok := parseOptionalInt(r.URL.Query().Get("limit"), 50)
	if !ok || limit < 1 || limit > 500 {
		writeAdminValidationError(w, requestID)
		return pageQuery{}, false
	}
	offset, ok := parseOptionalInt(r.URL.Query().Get("offset"), 0)
	if !ok || offset < 0 {
		writeAdminValidationError(w, requestID)
		return pageQuery{}, false
	}
	return pageQuery{limit: limit, offset: offset}, true
}

func parseOptionalInt(raw string, defaultValue int) (int, bool) {
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil
}
func parseOptionalBool(w http.ResponseWriter, r *http.Request, requestID, name string) (*bool, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		writeAdminValidationError(w, requestID)
		return nil, false
	}
	return &value, true
}
func parseOptionalTime(w http.ResponseWriter, r *http.Request, requestID, name string) (*time.Time, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || value.Location() != time.UTC {
		writeAdminValidationError(w, requestID)
		return nil, false
	}
	return &value, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, requestID string, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, requestID, http.StatusBadRequest, domain.ErrorCodeInvalidJSON, "Invalid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, requestID, http.StatusBadRequest, domain.ErrorCodeInvalidJSON, "Invalid JSON")
		return false
	}
	return true
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
func writeList(w http.ResponseWriter, data any, pagination application.Pagination) {
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "pagination": pagination})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, requestID string, status int, code domain.ErrorCode, message string) {
	errorBody := map[string]any{
		"code":    code,
		"message": message,
	}
	if requestID != "" {
		w.Header().Set(adminRequestIDHeader, requestID)
		errorBody["request_id"] = requestID
	}
	writeJSON(w, status, map[string]any{"error": errorBody})
}
func methodNotAllowed(w http.ResponseWriter, requestID, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, requestID, http.StatusMethodNotAllowed, domain.ErrorCodeMethodNotAllowed, "Method is not allowed")
}

func writeAdminValidationError(w http.ResponseWriter, requestID string) {
	writeError(
		w,
		requestID,
		http.StatusBadRequest,
		domain.ErrorCodeAdminValidationError,
		"Invalid admin request",
	)
}
func writeApplicationError(w http.ResponseWriter, requestID string, err error) {
	applicationError, ok := ports.AsApplicationError(err)
	if !ok {
		writeError(
			w,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}
	writeError(
		w,
		requestID,
		httptransport.StatusForApplicationError(applicationError),
		applicationError.Code,
		applicationError.SafeMessage,
	)
}
