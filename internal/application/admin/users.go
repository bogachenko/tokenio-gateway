package admin

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func userState(u domain.User) domain.AuditState {
	return domain.AuditState{"id": u.ID, "external_billing_user_id": u.ExternalBillingUserID, "email": u.Email, "name": u.Name, "enabled": u.Enabled, "created_at": u.CreatedAt, "updated_at": u.UpdatedAt, "disabled_at": u.DisabledAt}
}
func apiKeyState(k domain.APIKeyRecord) domain.AuditState {
	return domain.AuditState{"id": k.ID, "user_id": k.UserID, "name": k.Name, "key_prefix": k.KeyPrefix, "enabled": k.Enabled, "created_at": k.CreatedAt, "updated_at": k.UpdatedAt, "last_used_at": k.LastUsedAt, "revoked_at": k.RevokedAt, "expires_at": k.ExpiresAt}
}
func apiKeyView(k domain.APIKeyRecord) APIKeyView {
	return APIKeyView{ID: k.ID, UserID: k.UserID, Name: k.Name, KeyPrefix: k.KeyPrefix, Enabled: k.Enabled, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt, ExpiresAt: k.ExpiresAt}
}

func (s *Service) ListUsers(ctx context.Context, input UserListInput) (ListResult[domain.User], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[domain.User]{}, err
	}
	page, err := s.deps.Users.ListUsers(ctx, ports.UserListFilter{Enabled: input.Enabled, Email: input.Email, Page: pageReq})
	if err != nil {
		return ListResult[domain.User]{}, mapStoreError(err)
	}
	return listResult(page, pageReq), nil
}
func (s *Service) CreateUser(ctx context.Context, command CommandContext, input CreateUserInput) (domain.User, error) {
	if validateCommand(command) != nil || isBlank(input.ExternalBillingUserID) {
		return domain.User{}, ErrInvalidRequest
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.User{}, err
	}
	id := input.ID
	if id == "" {
		id = stableID("usr_", command.RequestID, input.ExternalBillingUserID)
	}
	if isBlank(id) {
		return domain.User{}, ErrInvalidRequest
	}
	u := domain.User{ID: id, ExternalBillingUserID: input.ExternalBillingUserID, Email: input.Email, Name: input.Name, Enabled: true, CreatedAt: at, UpdatedAt: at}
	audit := auditContext(command, domain.AuditActionUserCreate, "user", id, nil, userState(u), at)
	created, err := s.deps.Users.CreateUserWithAudit(ctx, u, audit)
	if err != nil {
		return domain.User{}, mapStoreError(err)
	}
	return created, nil
}
func (s *Service) SetUserEnabled(ctx context.Context, command CommandContext, userID string, enabled bool) (domain.User, error) {
	if validateCommand(command) != nil || isBlank(userID) {
		return domain.User{}, ErrInvalidRequest
	}
	cur, err := s.deps.Users.FindByID(ctx, userID)
	if err != nil {
		return domain.User{}, mapStoreError(err)
	}
	if cur == nil {
		return domain.User{}, ErrNotFound
	}
	if cur.Enabled == enabled {
		return domain.User{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return domain.User{}, err
	}
	next := *cur
	next.Enabled = enabled
	next.UpdatedAt = at
	if enabled {
		next.DisabledAt = nil
	} else {
		next.DisabledAt = &at
	}
	action := domain.AuditActionUserDisable
	if enabled {
		action = domain.AuditActionUserEnable
	}
	audit := auditContext(command, action, "user", userID, userState(*cur), userState(next), at)
	updated, err := s.deps.Users.CompareAndSwapUserWithAudit(ctx, *cur, next, audit)
	if err != nil {
		return domain.User{}, mapStoreError(err)
	}
	return updated, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, userID string, limit, offset int) (ListResult[APIKeyView], error) {
	if isBlank(userID) {
		return ListResult[APIKeyView]{}, ErrInvalidRequest
	}
	pageReq, err := normalizePage(limit, offset)
	if err != nil {
		return ListResult[APIKeyView]{}, err
	}
	page, err := s.deps.APIKeys.ListAPIKeys(ctx, ports.APIKeyListFilter{UserID: userID, Page: pageReq})
	if err != nil {
		return ListResult[APIKeyView]{}, mapStoreError(err)
	}
	items := make([]APIKeyView, len(page.Items))
	for i, k := range page.Items {
		items[i] = apiKeyView(k)
	}
	return ListResult[APIKeyView]{Data: items, Pagination: Pagination{Limit: pageReq.Limit, Offset: pageReq.Offset, Total: page.Total}}, nil
}
func (s *Service) CreateAPIKey(ctx context.Context, command CommandContext, input CreateAPIKeyInput) (CreatedAPIKey, error) {
	if validateCommand(command) != nil || isBlank(input.UserID) || isBlank(input.Name) || optionalUTC(input.ExpiresAt) != nil {
		return CreatedAPIKey{}, ErrInvalidRequest
	}
	u, err := s.deps.Users.FindByID(ctx, input.UserID)
	if err != nil {
		return CreatedAPIKey{}, mapStoreError(err)
	}
	if u == nil {
		return CreatedAPIKey{}, ErrNotFound
	}
	if !u.Enabled {
		return CreatedAPIKey{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(at) {
		return CreatedAPIKey{}, ErrInvalidRequest
	}
	raw, err := s.deps.KeyGenerator.GenerateAPIKey()
	if err != nil {
		return CreatedAPIKey{}, ErrInternal
	}
	if !validGeneratedAPIKey(raw) {
		return CreatedAPIKey{}, ErrInternal
	}
	hash := s.deps.Hasher.Hash(raw)
	if len(hash) != 64 {
		return CreatedAPIKey{}, ErrInternal
	}
	id := stableID("ak_", command.RequestID, input.UserID, input.Name, at.Format(time.RFC3339Nano))
	prefix := displayPrefix(raw)
	record := domain.APIKeyRecord{ID: id, UserID: input.UserID, Name: input.Name, KeyHash: hash, KeyPrefix: prefix, Enabled: true, CreatedAt: at, UpdatedAt: at, ExpiresAt: input.ExpiresAt}
	audit := auditContext(command, domain.AuditActionAPIKeyCreate, "api_key", id, nil, apiKeyState(record), at)
	if err := s.deps.APIKeys.CreateAPIKeyWithAudit(ctx, record, audit); err != nil {
		return CreatedAPIKey{}, mapStoreError(err)
	}
	return CreatedAPIKey{ID: record.ID, UserID: record.UserID, Name: record.Name, APIKey: raw, KeyPrefix: record.KeyPrefix, CreatedAt: record.CreatedAt}, nil
}
func (s *Service) RevokeAPIKey(ctx context.Context, command CommandContext, id string) (APIKeyView, error) {
	if validateCommand(command) != nil || isBlank(id) {
		return APIKeyView{}, ErrInvalidRequest
	}
	cur, err := s.deps.APIKeys.FindAPIKeyByID(ctx, id)
	if err != nil {
		return APIKeyView{}, mapStoreError(err)
	}
	if cur == nil {
		return APIKeyView{}, ErrNotFound
	}
	if !cur.Enabled || cur.RevokedAt != nil {
		return APIKeyView{}, ErrStateConflict
	}
	at, err := nowUTC(s.deps.Clock)
	if err != nil {
		return APIKeyView{}, err
	}
	next := *cur
	next.Enabled = false
	next.RevokedAt = &at
	next.UpdatedAt = at
	audit := auditContext(command, domain.AuditActionAPIKeyRevoke, "api_key", id, apiKeyState(*cur), apiKeyState(next), at)
	updated, err := s.deps.APIKeys.CompareAndSwapAPIKeyWithAudit(ctx, *cur, next, audit)
	if err != nil {
		return APIKeyView{}, mapStoreError(err)
	}
	return apiKeyView(updated), nil
}
func validGeneratedAPIKey(raw string) bool {
	if !strings.HasPrefix(raw, "sk_live_") {
		return false
	}
	payload := raw[len("sk_live_"):]
	if b, err := base64.RawURLEncoding.DecodeString(payload); err == nil && len(b) >= 32 {
		return true
	}
	if b, err := hex.DecodeString(payload); err == nil && len(b) >= 32 {
		return true
	}
	return false
}
func displayPrefix(raw string) string {
	n := len("sk_live_") + 8
	if n > len(raw) {
		n = len(raw)
	}
	return raw[:n] + "..."
}
