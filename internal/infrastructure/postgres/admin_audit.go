package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const insertAdminAuditSQL = `
INSERT INTO tokenio_admin_audit_log (
    id,
    admin_subject,
    action,
    entity_type,
    entity_id,
    before_state,
    after_state,
    reason,
    request_id,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10)
`

const adminAuditColumns = `
    id,
    admin_subject,
    action,
    entity_type,
    entity_id,
    before_state,
    after_state,
    COALESCE(reason, '') AS reason,
    request_id,
    created_at
`

func normalizeAdminWriteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrStoreConflict) {
		return ports.ErrAdminConflict
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ports.ErrNotFound) ||
		errors.Is(err, ports.ErrAdminConflict) ||
		errors.Is(err, ports.ErrAdminStateConflict) ||
		errors.Is(err, ports.ErrStoreUnavailable) ||
		errors.Is(err, ports.ErrStoreContractViolation) {
		return err
	}

	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23503", "23505", "23P01":
			return ports.ErrAdminConflict
		case "40001", "40P01":
			return ports.ErrAdminStateConflict
		case "22000", "22023", "23502", "23514", "42P01", "42703":
			return ports.ErrStoreContractViolation
		default:
			return NormalizeError(err)
		}
	}
	return NormalizeError(err)
}

func validateAdminPage(page ports.PageRequest) error {
	if page.Limit <= 0 || page.Offset < 0 {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateAuditContext(audit domain.AuditContext) error {
	if audit.ID == "" ||
		audit.AdminSubject == "" ||
		audit.Action == "" ||
		audit.EntityType == "" ||
		audit.EntityID == "" ||
		audit.RequestID == "" ||
		!isAdminUTCTime(audit.CreatedAt) ||
		audit.BeforeState == nil ||
		audit.AfterState == nil {
		return ports.ErrStoreContractViolation
	}
	if auditReasonRequired(audit.Action) &&
		strings.TrimSpace(audit.Reason) == "" {
		return ports.ErrStoreContractViolation
	}
	if auditStateContainsSecret(audit.BeforeState) ||
		auditStateContainsSecret(audit.AfterState) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func auditReasonRequired(action domain.AuditAction) bool {
	switch action {
	case domain.AuditActionResellerBalanceAdjust,
		domain.AuditActionResellerBalanceSet,
		domain.AuditActionUsageResolveBillable,
		domain.AuditActionUsageResolveFailed,
		domain.AuditActionUsageResolveCharged:
		return true
	default:
		return false
	}
}

func insertAdminAudit(
	ctx context.Context,
	tx pgx.Tx,
	audit domain.AuditContext,
) error {
	if err := validateAuditContext(audit); err != nil {
		return err
	}

	beforeBody, err := encodeAuditState(audit.BeforeState)
	if err != nil {
		return err
	}
	afterBody, err := encodeAuditState(audit.AfterState)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		ctx,
		insertAdminAuditSQL,
		audit.ID,
		audit.AdminSubject,
		string(audit.Action),
		audit.EntityType,
		audit.EntityID,
		beforeBody,
		afterBody,
		nullIfEmpty(audit.Reason),
		audit.RequestID,
		audit.CreatedAt,
	); err != nil {
		return normalizeAdminWriteError(err)
	}
	return nil
}

func encodeAuditState(state domain.AuditState) ([]byte, error) {
	if state == nil || auditStateContainsSecret(state) {
		return nil, ports.ErrStoreContractViolation
	}
	body, err := json.Marshal(state)
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	decoded, err := decodeAuditState(body)
	if err != nil {
		return nil, err
	}
	if !auditStateEqual(state, decoded) {
		return nil, ports.ErrStoreContractViolation
	}
	return body, nil
}

func decodeAuditState(raw []byte) (domain.AuditState, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return domain.AuditState{}, nil
	}
	if len(trimmed) < 2 ||
		trimmed[0] != '{' ||
		trimmed[len(trimmed)-1] != '}' {
		return nil, ports.ErrStoreContractViolation
	}

	var state domain.AuditState
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&state); err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ports.ErrStoreContractViolation
	}
	if state == nil {
		state = domain.AuditState{}
	}
	if auditStateContainsSecret(state) {
		return nil, ports.ErrStoreContractViolation
	}
	return state, nil
}

func auditStateEqual(left domain.AuditState, right domain.AuditState) bool {
	leftBody, err := canonicalAuditStateJSON(left)
	if err != nil {
		return false
	}
	rightBody, err := canonicalAuditStateJSON(right)
	if err != nil {
		return false
	}
	return bytes.Equal(leftBody, rightBody)
}

func canonicalAuditStateJSON(state domain.AuditState) ([]byte, error) {
	if state == nil || auditStateContainsSecret(state) {
		return nil, ports.ErrStoreContractViolation
	}
	body, err := json.Marshal(state)
	if err != nil {
		return nil, ports.ErrStoreContractViolation
	}

	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, ports.ErrStoreContractViolation
	}
	return json.Marshal(normalized)
}

func auditStateContainsSecret(state domain.AuditState) bool {
	return containsForbiddenAuditKey(map[string]any(state))
}

func containsForbiddenAuditKey(value any) bool {
	switch typed := value.(type) {
	case domain.AuditState:
		return containsForbiddenAuditKey(map[string]any(typed))
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case
				"raw_api_key",
				"api_key",
				"key_hash",
				"encrypted_raw_key",
				"encryption_nonce",
				"encryption_key",
				"billing_jwt",
				"billing_service_token",
				"admin_token",
				"authorization":
				return true
			}
			if containsForbiddenAuditKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsForbiddenAuditKey(child) {
				return true
			}
		}
	}
	return false
}

func isAdminUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func adminCanonicalTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := value.UTC()
	return &canonical
}

func sameAdminTimePointer(left *time.Time, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.UTC().Equal(right.UTC())
	}
}

func sameUserPersistence(left domain.User, right domain.User) bool {
	return left.ID == right.ID &&
		left.ExternalBillingUserID == right.ExternalBillingUserID &&
		left.Email == right.Email &&
		left.Name == right.Name &&
		left.Enabled == right.Enabled &&
		left.CreatedAt.UTC().Equal(right.CreatedAt.UTC()) &&
		left.UpdatedAt.UTC().Equal(right.UpdatedAt.UTC()) &&
		sameAdminTimePointer(left.DisabledAt, right.DisabledAt)
}

func sameAPIKeyPersistence(
	left domain.APIKeyRecord,
	right domain.APIKeyRecord,
) bool {
	return left.ID == right.ID &&
		left.UserID == right.UserID &&
		left.Name == right.Name &&
		left.KeyHash == right.KeyHash &&
		left.KeyPrefix == right.KeyPrefix &&
		left.Enabled == right.Enabled &&
		left.CreatedAt.UTC().Equal(right.CreatedAt.UTC()) &&
		left.UpdatedAt.UTC().Equal(right.UpdatedAt.UTC()) &&
		sameAdminTimePointer(left.LastUsedAt, right.LastUsedAt) &&
		sameAdminTimePointer(left.RevokedAt, right.RevokedAt) &&
		sameAdminTimePointer(left.ExpiresAt, right.ExpiresAt)
}

func adminUserState(value domain.User) domain.AuditState {
	return domain.AuditState{
		"id":                       value.ID,
		"external_billing_user_id": value.ExternalBillingUserID,
		"email":                    value.Email,
		"name":                     value.Name,
		"enabled":                  value.Enabled,
		"created_at":               value.CreatedAt.UTC(),
		"updated_at":               value.UpdatedAt.UTC(),
		"disabled_at":              adminCanonicalTimePointer(value.DisabledAt),
	}
}

func adminAPIKeyState(value domain.APIKeyRecord) domain.AuditState {
	return domain.AuditState{
		"id":           value.ID,
		"user_id":      value.UserID,
		"name":         value.Name,
		"key_prefix":   value.KeyPrefix,
		"enabled":      value.Enabled,
		"created_at":   value.CreatedAt.UTC(),
		"updated_at":   value.UpdatedAt.UTC(),
		"last_used_at": adminCanonicalTimePointer(value.LastUsedAt),
		"revoked_at":   adminCanonicalTimePointer(value.RevokedAt),
		"expires_at":   adminCanonicalTimePointer(value.ExpiresAt),
	}
}

func validateAuditForEntity(
	audit domain.AuditContext,
	action domain.AuditAction,
	entityType string,
	entityID string,
	before domain.AuditState,
	after domain.AuditState,
	at time.Time,
) error {
	if err := validateAuditContext(audit); err != nil {
		return err
	}
	if audit.Action != action ||
		audit.EntityType != entityType ||
		audit.EntityID != entityID ||
		!audit.CreatedAt.Equal(at) ||
		!auditStateEqual(audit.BeforeState, before) ||
		!auditStateEqual(audit.AfterState, after) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func scanAdminAuditEntry(row rowScanner) (domain.AdminAuditEntry, error) {
	var value domain.AdminAuditEntry
	var action string
	var beforeRaw []byte
	var afterRaw []byte

	if err := row.Scan(
		&value.ID,
		&value.AdminSubject,
		&action,
		&value.EntityType,
		&value.EntityID,
		&beforeRaw,
		&afterRaw,
		&value.Reason,
		&value.RequestID,
		&value.CreatedAt,
	); err != nil {
		return domain.AdminAuditEntry{}, normalizeRegistryReadError(err)
	}

	before, err := decodeAuditState(beforeRaw)
	if err != nil {
		return domain.AdminAuditEntry{}, err
	}
	after, err := decodeAuditState(afterRaw)
	if err != nil {
		return domain.AdminAuditEntry{}, err
	}

	value.Action = domain.AuditAction(action)
	value.BeforeState = before
	value.AfterState = after
	value.CreatedAt = value.CreatedAt.UTC()

	if value.ID == "" ||
		value.AdminSubject == "" ||
		value.Action == "" ||
		value.EntityType == "" ||
		value.EntityID == "" ||
		value.RequestID == "" ||
		value.CreatedAt.IsZero() {
		return domain.AdminAuditEntry{}, ports.ErrStoreContractViolation
	}
	return value, nil
}

func cloneAuditState(state domain.AuditState) domain.AuditState {
	body, err := canonicalAuditStateJSON(state)
	if err != nil {
		return nil
	}
	decoded, err := decodeAuditState(body)
	if err != nil {
		return nil
	}
	return decoded
}

func sameAuditEntry(
	left domain.AdminAuditEntry,
	right domain.AdminAuditEntry,
) bool {
	return left.ID == right.ID &&
		left.AdminSubject == right.AdminSubject &&
		left.Action == right.Action &&
		left.EntityType == right.EntityType &&
		left.EntityID == right.EntityID &&
		auditStateEqual(left.BeforeState, right.BeforeState) &&
		auditStateEqual(left.AfterState, right.AfterState) &&
		left.Reason == right.Reason &&
		left.RequestID == right.RequestID &&
		left.CreatedAt.UTC().Equal(right.CreatedAt.UTC())
}
