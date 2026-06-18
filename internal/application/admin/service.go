package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	defaultLimit = 50
	maxLimit     = 500
	currencyRUB  = "RUB"
)

var billingChargeIDPattern = regexp.MustCompile(`^billchg_[0-9a-f]{64}$`)

type Service struct {
	deps         Dependencies
	provisioning *ProvisioningQueryService
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Users == nil || deps.APIKeys == nil || deps.Provisionings == nil || deps.RouteEvents == nil || deps.Resellers == nil || deps.Routes == nil || deps.Prices == nil || deps.PriceValidator == nil || deps.UsagePolicy == nil || deps.Ledger == nil || deps.Audit == nil || deps.Secrets == nil || deps.AdapterSupport == nil || deps.KeyGenerator == nil || deps.Hasher == nil || deps.Clock == nil || deps.BatchRetrier == nil {
		return nil, fmt.Errorf("%w: dependency", ErrInternal)
	}
	provisioning, err := NewProvisioningQueryService(
		deps.Provisionings,
	)
	if err != nil {
		return nil, err
	}
	return &Service{
		deps:         deps,
		provisioning: provisioning,
	}, nil
}

func validateCommand(c CommandContext) error {
	if !strings.HasPrefix(c.RequestID, "admreq_") || len(c.RequestID) == len("admreq_") || c.AdminSubject != "admin_token" {
		return ErrInvalidRequest
	}
	return nil
}
func normalizePage(limit, offset int) (ports.PageRequest, error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit || offset < 0 {
		return ports.PageRequest{}, ErrInvalidRequest
	}
	return ports.PageRequest{Limit: limit, Offset: offset}, nil
}
func listResult[T any](page ports.Page[T], req ports.PageRequest) ListResult[T] {
	return ListResult[T]{Data: page.Items, Pagination: Pagination{Limit: req.Limit, Offset: req.Offset, Total: page.Total}}
}
func isBlank(v string) bool { return strings.TrimSpace(v) == "" }
func nowUTC(clock ports.Clock) (time.Time, error) {
	t := clock.Now()
	if t.IsZero() {
		return time.Time{}, ErrInternal
	}
	return t.UTC(), nil
}
func stableID(prefix string, values ...string) string {
	h := sha256.New()
	for _, v := range values {
		_, _ = h.Write([]byte(v))
		_, _ = h.Write([]byte{0})
	}
	return prefix + hex.EncodeToString(h.Sum(nil))
}
func auditContext(c CommandContext, action domain.AuditAction, entityType, entityID string, before, after domain.AuditState, at time.Time) domain.AuditContext {
	if before == nil {
		before = domain.AuditState{}
	}
	if after == nil {
		after = domain.AuditState{}
	}
	return domain.AuditContext{ID: stableID("audit_", c.RequestID, string(action), entityType, entityID), AdminSubject: c.AdminSubject, Action: action, EntityType: entityType, EntityID: entityID, BeforeState: before, AfterState: after, RequestID: c.RequestID, CreatedAt: at.UTC()}
}
func auditContextWithReason(
	c CommandContext,
	action domain.AuditAction,
	entityType string,
	entityID string,
	before domain.AuditState,
	after domain.AuditState,
	reason string,
	at time.Time,
) domain.AuditContext {
	audit := auditContext(
		c,
		action,
		entityType,
		entityID,
		before,
		after,
		at,
	)
	audit.Reason = reason
	return audit
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ports.ErrAdminConflict):
		return ErrConflict
	case errors.Is(err, ports.ErrAdminStateConflict):
		return ErrStateConflict
	default:
		return ErrStoreUnavailable
	}
}
func checkedAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, ErrInvalidRequest
	}
	return a + b, nil
}
func checkedSub(a, b int64) (int64, error) {
	if b > 0 && a < math.MinInt64+b || b < 0 && a > math.MaxInt64+b {
		return 0, ErrInvalidRequest
	}
	return a - b, nil
}
func requireUTC(t time.Time) error {
	if t.IsZero() || t.Location() != time.UTC {
		return ErrInvalidRequest
	}
	return nil
}
func optionalUTC(t *time.Time) error {
	if t == nil {
		return nil
	}
	return requireUTC(*t)
}
func validateWindow(from, to *time.Time) error {
	if optionalUTC(from) != nil || optionalUTC(to) != nil {
		return ErrInvalidRequest
	}
	if from != nil && to != nil && !from.Before(*to) {
		return ErrInvalidRequest
	}
	return nil
}
func validateProviderType(v domain.ProviderType) bool {
	switch v {
	case domain.ProviderOpenAI, domain.ProviderOpenRouter, domain.ProviderTogether, domain.ProviderGroq, domain.ProviderOllama, domain.ProviderLMStudio, domain.ProviderVLLM, domain.ProviderGemini, domain.ProviderAnthropic, domain.ProviderHydra:
		return true
	}
	return false
}
func validateAPIFamily(v domain.APIFamily) bool {
	switch v {
	case domain.APIFamilyOpenAICompatible, domain.APIFamilyGeminiNative, domain.APIFamilyAnthropicNative, domain.APIFamilyOllamaNative:
		return true
	}
	return false
}
func validateEndpoint(v domain.EndpointKind) bool {
	switch v {
	case domain.EndpointChat, domain.EndpointEmbeddings, domain.EndpointImagesGeneration:
		return true
	}
	return false
}
func routeDefaultMaxOutputTokensValid(
	route domain.Route,
) bool {
	switch route.EndpointKind {
	case domain.EndpointChat:
		return route.DefaultMaxOutputTokens > 0
	case domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return route.DefaultMaxOutputTokens == 0
	default:
		return false
	}
}

func routeEndpointCapabilityValid(
	route domain.Route,
) bool {
	capabilities := route.Capabilities

	if capabilities.ToolChoice && !capabilities.Tools {
		return false
	}
	if capabilities.JSONSchema &&
		!capabilities.ResponseFormat {
		return false
	}

	switch route.EndpointKind {
	case domain.EndpointChat:
		return capabilities.Chat &&
			!capabilities.Embeddings &&
			!capabilities.ImagesGeneration
	case domain.EndpointEmbeddings:
		return !capabilities.Chat &&
			capabilities.Embeddings &&
			!capabilities.ImagesGeneration &&
			!hasAncillaryCapabilities(capabilities)
	case domain.EndpointImagesGeneration:
		return !capabilities.Chat &&
			!capabilities.Embeddings &&
			capabilities.ImagesGeneration &&
			!hasAncillaryCapabilities(capabilities)
	default:
		return false
	}
}

func hasAncillaryCapabilities(
	capabilities domain.CapabilitySet,
) bool {
	return capabilities.Tools ||
		capabilities.ToolChoice ||
		capabilities.ResponseFormat ||
		capabilities.JSONSchema ||
		capabilities.ImageInput ||
		capabilities.AudioInput ||
		capabilities.FileInput ||
		capabilities.VideoInput ||
		capabilities.Reasoning
}

func validateRoute(r domain.Route) error {
	if isBlank(r.ID) || isBlank(r.ResellerID) || !validateProviderType(r.ProviderType) || !validateAPIFamily(r.APIFamily) || !validateEndpoint(r.EndpointKind) || isBlank(r.ClientModel) || isBlank(r.ProviderModel) {
		return ErrInvalidRequest
	}
	if r.ModelRewritePolicy != domain.ModelRewritePolicyNone && r.ModelRewritePolicy != domain.ModelRewritePolicyProviderModel {
		return ErrInvalidRequest
	}
	if r.ProviderModel != r.ClientModel && r.ModelRewritePolicy != domain.ModelRewritePolicyProviderModel {
		return ErrInvalidRequest
	}
	if r.Priority < 0 || r.RequestsPerMinute < 0 || r.TokensPerMinute < 0 || r.ConcurrentRequests < 0 || r.DefaultMaxOutputTokens < 0 {
		return ErrInvalidRequest
	}
	if !routeDefaultMaxOutputTokensValid(r) {
		return ErrInvalidRequest
	}
	if !routeEndpointCapabilityValid(r) {
		return ErrInvalidRequest
	}
	if requireUTC(r.CreatedAt) != nil || requireUTC(r.UpdatedAt) != nil || optionalUTC(r.CooldownUntil) != nil || optionalUTC(r.LastErrorAt) != nil || optionalUTC(r.DisabledAt) != nil {
		return ErrInvalidRequest
	}
	return nil
}
func (s *Service) validatePrice(p domain.RoutePrice) error {
	if err := s.deps.PriceValidator.ValidateRoutePrice(p); err != nil {
		return ErrInvalidRequest
	}
	if math.IsNaN(p.MarkupCoefficient) || math.IsInf(p.MarkupCoefficient, 0) || p.MarkupCoefficient <= 0 {
		return ErrInvalidRequest
	}
	if p.ImageGenerationUnitKind != domain.ImageGenerationUnitKindNone && p.ImageGenerationUnitKind != domain.ImageGenerationUnitKindGeneratedImage {
		return ErrInvalidRequest
	}
	if requireUTC(p.CreatedAt) != nil || requireUTC(p.UpdatedAt) != nil {
		return ErrInvalidRequest
	}
	return nil
}
func validBillingChargeID(v string) bool { return billingChargeIDPattern.MatchString(v) }
