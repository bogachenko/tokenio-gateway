package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminResellerRepositoryIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	repository, err := NewAdminResellerRepository(db)
	if err != nil {
		t.Fatalf("NewAdminResellerRepository: %v", err)
	}
	audits, err := NewAdminAuditStore(db)
	if err != nil {
		t.Fatalf("NewAdminAuditStore: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	resellerID := "admin-reseller-" + suffix
	rollbackID := "admin-reseller-rollback-" + suffix
	adminSubject := "admin-reseller-subject-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id IN ($1, $2)",
			resellerID,
			rollbackID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_admin_audit_log WHERE admin_subject = $1",
			adminSubject,
		)
	})

	requested := domain.Reseller{
		ID:                  resellerID,
		Name:                "Primary",
		ProviderType:        domain.ProviderOpenAI,
		BaseURL:             "https://example.test",
		APIKeyEnv:           "ADMIN_RESELLER_KEY_" + suffix,
		Enabled:             true,
		BalanceCents:        1000,
		ReservedCents:       0,
		MinimumBalanceCents: 100,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	createAudit := adminResellerAuditContext(
		"audit-reseller-create-"+suffix,
		adminSubject,
		domain.AuditActionResellerCreate,
		resellerID,
		domain.AuditState{},
		adminResellerApplicationState(requested),
		"admreq-reseller-create-"+suffix,
		now,
	)
	created, err := repository.CreateResellerWithAudit(
		ctx,
		requested,
		createAudit,
	)
	if err != nil {
		t.Fatalf("CreateResellerWithAudit: %v", err)
	}
	if !sameAdminReseller(created, requested) {
		t.Fatalf("created = %+v", created)
	}

	updateAt := now.Add(time.Second)
	updatedInput := created
	updatedInput.Name = "Updated"
	updatedInput.MinimumBalanceCents = 200
	updatedInput.UpdatedAt = updateAt
	updateAudit := adminResellerAuditContext(
		"audit-reseller-update-"+suffix,
		adminSubject,
		domain.AuditActionResellerUpdate,
		resellerID,
		adminResellerApplicationState(created),
		adminResellerApplicationState(updatedInput),
		"admreq-reseller-update-"+suffix,
		updateAt,
	)
	updated, err := repository.CompareAndSwapResellerWithAudit(
		ctx,
		created,
		updatedInput,
		updateAudit,
	)
	if err != nil {
		t.Fatalf("update reseller: %v", err)
	}

	disableAt := now.Add(2 * time.Second)
	disabledInput := updated
	disabledInput.Enabled = false
	disabledInput.DisabledAt = &disableAt
	disabledInput.UpdatedAt = disableAt
	disableAudit := adminResellerAuditContext(
		"audit-reseller-disable-"+suffix,
		adminSubject,
		domain.AuditActionResellerDisable,
		resellerID,
		adminResellerApplicationState(updated),
		adminResellerApplicationState(disabledInput),
		"admreq-reseller-disable-"+suffix,
		disableAt,
	)
	disabled, err := repository.CompareAndSwapResellerWithAudit(
		ctx,
		updated,
		disabledInput,
		disableAudit,
	)
	if err != nil {
		t.Fatalf("disable reseller: %v", err)
	}

	enableAt := now.Add(3 * time.Second)
	enabledInput := disabled
	enabledInput.Enabled = true
	enabledInput.DisabledAt = nil
	enabledInput.UpdatedAt = enableAt
	enableAudit := adminResellerAuditContext(
		"audit-reseller-enable-"+suffix,
		adminSubject,
		domain.AuditActionResellerEnable,
		resellerID,
		adminResellerApplicationState(disabled),
		adminResellerApplicationState(enabledInput),
		"admreq-reseller-enable-"+suffix,
		enableAt,
	)
	enabled, err := repository.CompareAndSwapResellerWithAudit(
		ctx,
		disabled,
		enabledInput,
		enableAudit,
	)
	if err != nil {
		t.Fatalf("enable reseller: %v", err)
	}

	balanceAt := now.Add(4 * time.Second)
	balanceInput := enabled
	balanceInput.BalanceCents += 250
	balanceInput.UpdatedAt = balanceAt
	balanceAudit := adminResellerAuditContext(
		"audit-reseller-balance-"+suffix,
		adminSubject,
		domain.AuditActionResellerBalanceAdjust,
		resellerID,
		adminResellerApplicationState(enabled),
		adminResellerApplicationState(balanceInput),
		"admreq-reseller-balance-"+suffix,
		balanceAt,
	)
	balanceAudit.Reason = "integration balance adjustment"
	balanced, err := repository.CompareAndSwapResellerWithAudit(
		ctx,
		enabled,
		balanceInput,
		balanceAudit,
	)
	if err != nil {
		t.Fatalf("adjust balance: %v", err)
	}
	if balanced.BalanceCents != 1250 {
		t.Fatalf("balance = %d, want 1250", balanced.BalanceCents)
	}

	staleNext := enabled
	staleNext.BalanceCents++
	staleNext.UpdatedAt = now.Add(5 * time.Second)
	staleAudit := adminResellerAuditContext(
		"audit-reseller-stale-"+suffix,
		adminSubject,
		domain.AuditActionResellerBalanceAdjust,
		resellerID,
		adminResellerApplicationState(enabled),
		adminResellerApplicationState(staleNext),
		"admreq-reseller-stale-"+suffix,
		staleNext.UpdatedAt,
	)
	staleAudit.Reason = "integration stale balance adjustment"
	_, err = repository.CompareAndSwapResellerWithAudit(
		ctx,
		enabled,
		staleNext,
		staleAudit,
	)
	if !errors.Is(err, ports.ErrAdminStateConflict) {
		t.Fatalf("stale CAS error = %v, want state conflict", err)
	}

	page, err := repository.ListResellers(
		ctx,
		ports.ResellerListFilter{
			ProviderType: domain.ProviderOpenAI,
			Enabled:      adminResellerBoolPointer(true),
			Page:         ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListResellers: %v", err)
	}
	if page.Total != 1 ||
		len(page.Items) != 1 ||
		page.Items[0].ID != resellerID {
		t.Fatalf("reseller page = %+v", page)
	}

	auditPage, err := audits.ListAuditEntries(
		ctx,
		ports.AuditListFilter{
			AdminSubject: adminSubject,
			Page:         ports.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	if auditPage.Total != 5 || len(auditPage.Items) != 5 {
		t.Fatalf("audit page = %+v", auditPage)
	}

	rollback := requested
	rollback.ID = rollbackID
	rollback.CreatedAt = now.Add(6 * time.Second)
	rollback.UpdatedAt = rollback.CreatedAt
	rollbackAudit := adminResellerAuditContext(
		createAudit.ID,
		adminSubject,
		domain.AuditActionResellerCreate,
		rollbackID,
		domain.AuditState{},
		adminResellerApplicationState(rollback),
		"admreq-reseller-rollback-"+suffix,
		rollback.CreatedAt,
	)
	_, err = repository.CreateResellerWithAudit(
		ctx,
		rollback,
		rollbackAudit,
	)
	if !errors.Is(err, ports.ErrAdminConflict) {
		t.Fatalf("audit collision error = %v, want conflict", err)
	}
	_, err = repository.FindResellerByID(ctx, rollbackID)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("rolled-back reseller error = %v, want not found", err)
	}
}

func adminResellerAuditContext(
	id string,
	adminSubject string,
	action domain.AuditAction,
	entityID string,
	before domain.AuditState,
	after domain.AuditState,
	requestID string,
	createdAt time.Time,
) domain.AuditContext {
	return domain.AuditContext{
		ID:           id,
		AdminSubject: adminSubject,
		Action:       action,
		EntityType:   "reseller",
		EntityID:     entityID,
		BeforeState:  before,
		AfterState:   after,
		RequestID:    requestID,
		CreatedAt:    createdAt,
	}
}

func adminResellerBoolPointer(value bool) *bool {
	return &value
}
