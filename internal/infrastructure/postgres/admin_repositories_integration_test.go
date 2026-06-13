package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminUserAPIKeyAuditIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	users, err := NewAdminUserRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := NewAdminAPIKeyRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	audits, err := NewAdminAuditStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "admin-user-" + suffix
	rollbackUserID := "admin-rollback-user-" + suffix
	keyID := "admin-key-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)
	adminSubject := "admin-token-" + suffix

	t.Cleanup(func() {
		for _, statement := range []struct {
			sql string
			arg string
		}{
			{
				"DELETE FROM tokenio_api_keys WHERE id = $1",
				keyID,
			},
			{
				"DELETE FROM tokenio_users WHERE id IN ($1, $2)",
				userID,
			},
		} {
			if strings.Contains(statement.sql, "$2") {
				_, _ = db.Exec(
					context.Background(),
					statement.sql,
					userID,
					rollbackUserID,
				)
			} else {
				_, _ = db.Exec(
					context.Background(),
					statement.sql,
					statement.arg,
				)
			}
		}
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_admin_audit_log WHERE request_id LIKE $1",
			"admreq-"+suffix+"%",
		)
	})

	user := domain.User{
		ID:                    userID,
		ExternalBillingUserID: "billing-" + suffix,
		Email:                 "user-" + suffix + "@example.test",
		Name:                  "Admin Test",
		Enabled:               true,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	createUserAudit := domain.AuditContext{
		ID:           "audit-user-create-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionUserCreate,
		EntityType:   "user",
		EntityID:     userID,
		BeforeState:  domain.AuditState{},
		AfterState:   adminUserState(user),
		RequestID:    "admreq-" + suffix + "-user-create",
		CreatedAt:    now,
	}
	created, err := users.CreateUserWithAudit(
		ctx,
		user,
		createUserAudit,
	)
	if err != nil {
		t.Fatalf("CreateUserWithAudit: %v", err)
	}
	if !sameUserPersistence(created, user) {
		t.Fatalf("created user = %+v", created)
	}

	disabledAt := now.Add(time.Second)
	disabled := created
	disabled.Enabled = false
	disabled.DisabledAt = &disabledAt
	disabled.UpdatedAt = disabledAt
	disableAudit := domain.AuditContext{
		ID:           "audit-user-disable-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionUserDisable,
		EntityType:   "user",
		EntityID:     userID,
		BeforeState:  adminUserState(created),
		AfterState:   adminUserState(disabled),
		RequestID:    "admreq-" + suffix + "-user-disable",
		CreatedAt:    disabledAt,
	}
	updated, err := users.CompareAndSwapUserWithAudit(
		ctx,
		created,
		disabled,
		disableAudit,
	)
	if err != nil {
		t.Fatalf("CompareAndSwapUserWithAudit: %v", err)
	}
	if !sameUserPersistence(updated, disabled) {
		t.Fatalf("updated user = %+v", updated)
	}

	staleNext := created
	staleNext.Enabled = false
	staleNext.DisabledAt = &disabledAt
	staleNext.UpdatedAt = disabledAt
	staleAudit := disableAudit
	staleAudit.ID = "audit-user-stale-" + suffix
	staleAudit.RequestID = "admreq-" + suffix + "-user-stale"
	_, err = users.CompareAndSwapUserWithAudit(
		ctx,
		created,
		staleNext,
		staleAudit,
	)
	if !errors.Is(err, ports.ErrAdminStateConflict) {
		t.Fatalf("stale CAS error = %v, want state conflict", err)
	}

	keyCreatedAt := now.Add(2 * time.Second)
	key := domain.APIKeyRecord{
		ID:        keyID,
		UserID:    userID,
		Name:      "Laptop",
		KeyHash:   strings.Repeat("a", 64),
		KeyPrefix: "sk_live_abcd...",
		Enabled:   true,
		CreatedAt: keyCreatedAt,
		UpdatedAt: keyCreatedAt,
	}
	createKeyAudit := domain.AuditContext{
		ID:           "audit-key-create-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionAPIKeyCreate,
		EntityType:   "api_key",
		EntityID:     keyID,
		BeforeState:  domain.AuditState{},
		AfterState:   adminAPIKeyState(key),
		RequestID:    "admreq-" + suffix + "-key-create",
		CreatedAt:    keyCreatedAt,
	}
	if err := keys.CreateAPIKeyWithAudit(
		ctx,
		key,
		createKeyAudit,
	); err != nil {
		t.Fatalf("CreateAPIKeyWithAudit: %v", err)
	}

	revokedAt := now.Add(3 * time.Second)
	revoked := key
	revoked.Enabled = false
	revoked.RevokedAt = &revokedAt
	revoked.UpdatedAt = revokedAt
	revokeAudit := domain.AuditContext{
		ID:           "audit-key-revoke-" + suffix,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionAPIKeyRevoke,
		EntityType:   "api_key",
		EntityID:     keyID,
		BeforeState:  adminAPIKeyState(key),
		AfterState:   adminAPIKeyState(revoked),
		RequestID:    "admreq-" + suffix + "-key-revoke",
		CreatedAt:    revokedAt,
	}
	revokedResult, err := keys.CompareAndSwapAPIKeyWithAudit(
		ctx,
		key,
		revoked,
		revokeAudit,
	)
	if err != nil {
		t.Fatalf("CompareAndSwapAPIKeyWithAudit: %v", err)
	}
	if !sameAPIKeyPersistence(revokedResult, revoked) {
		t.Fatalf("revoked key = %+v", revokedResult)
	}

	userPage, err := users.ListUsers(ctx, ports.UserListFilter{
		Enabled: boolPointer(false),
		Email:   user.Email,
		Page:    ports.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if userPage.Total != 1 ||
		len(userPage.Items) != 1 ||
		userPage.Items[0].ID != userID {
		t.Fatalf("user page = %+v", userPage)
	}

	keyPage, err := keys.ListAPIKeys(ctx, ports.APIKeyListFilter{
		UserID: userID,
		Page:   ports.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if keyPage.Total != 1 ||
		len(keyPage.Items) != 1 ||
		keyPage.Items[0].ID != keyID {
		t.Fatalf("key page = %+v", keyPage)
	}

	auditPage, err := audits.ListAuditEntries(
		ctx,
		ports.AuditListFilter{
			AdminSubject: adminSubject,
			CreatedFrom:  timePointer(now.Add(-time.Second)),
			CreatedTo:    timePointer(now.Add(10 * time.Second)),
			Page:         ports.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if auditPage.Total != 4 || len(auditPage.Items) != 4 {
		t.Fatalf("audit page = %+v", auditPage)
	}
	for _, entry := range auditPage.Items {
		if auditStateContainsSecret(entry.BeforeState) ||
			auditStateContainsSecret(entry.AfterState) {
			t.Fatalf("secret in audit entry: %+v", entry)
		}
	}

	rollbackUser := domain.User{
		ID:                    rollbackUserID,
		ExternalBillingUserID: "billing-rollback-" + suffix,
		Enabled:               true,
		CreatedAt:             now.Add(4 * time.Second),
		UpdatedAt:             now.Add(4 * time.Second),
	}
	rollbackAudit := domain.AuditContext{
		ID:           createUserAudit.ID,
		AdminSubject: adminSubject,
		Action:       domain.AuditActionUserCreate,
		EntityType:   "user",
		EntityID:     rollbackUserID,
		BeforeState:  domain.AuditState{},
		AfterState:   adminUserState(rollbackUser),
		RequestID:    "admreq-" + suffix + "-rollback",
		CreatedAt:    rollbackUser.CreatedAt,
	}
	_, err = users.CreateUserWithAudit(ctx, rollbackUser, rollbackAudit)
	if !errors.Is(err, ports.ErrAdminConflict) {
		t.Fatalf("audit collision error = %v, want conflict", err)
	}
	_, err = users.FindByID(ctx, rollbackUserID)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("rolled-back user error = %v, want not found", err)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func timePointer(value time.Time) *time.Time {
	return &value
}
