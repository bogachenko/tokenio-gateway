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

func TestAdminAuditReasonPersistenceAndRequirement(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)
	if err := db.ValidateSchema(ctx); err != nil {
		t.Fatalf("ValidateSchema: %v", err)
	}

	repository, err := NewAdminResellerRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	audits, err := NewAdminAuditStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	resellerID := "admin-reason-reseller-" + suffix
	adminSubject := "admin-reason-subject-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM tokenio_resellers WHERE id = $1", resellerID)
		_, _ = db.Exec(context.Background(), "DELETE FROM tokenio_admin_audit_log WHERE admin_subject = $1", adminSubject)
	})

	current := domain.Reseller{
		ID: resellerID, Name: "Reason test", ProviderType: domain.ProviderOpenAI,
		BaseURL: "https://example.test", APIKeyEnv: "ADMIN_REASON_KEY_" + suffix,
		Enabled: true, BalanceCents: 100, CreatedAt: now, UpdatedAt: now,
	}
	createAudit := domain.AuditContext{
		ID: "audit-reason-create-" + suffix, AdminSubject: adminSubject,
		Action: domain.AuditActionResellerCreate, EntityType: "reseller", EntityID: resellerID,
		BeforeState: domain.AuditState{}, AfterState: adminResellerApplicationState(current),
		RequestID: "admreq-reason-create-" + suffix, CreatedAt: now,
	}
	created, err := repository.CreateResellerWithAudit(ctx, current, createAudit)
	if err != nil {
		t.Fatalf("create reseller: %v", err)
	}

	next := created
	next.BalanceCents = 200
	next.UpdatedAt = now.Add(time.Second)
	missingReason := domain.AuditContext{
		ID: "audit-reason-missing-" + suffix, AdminSubject: adminSubject,
		Action: domain.AuditActionResellerBalanceSet, EntityType: "reseller", EntityID: resellerID,
		BeforeState: adminResellerApplicationState(created), AfterState: adminResellerApplicationState(next),
		RequestID: "admreq-reason-missing-" + suffix, CreatedAt: next.UpdatedAt,
	}
	if _, err := repository.CompareAndSwapResellerWithAudit(ctx, created, next, missingReason); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("missing reason error=%v", err)
	}

	const reason = "manual reconciliation"
	validAudit := missingReason
	validAudit.ID = "audit-reason-valid-" + suffix
	validAudit.RequestID = "admreq-reason-valid-" + suffix
	validAudit.Reason = reason
	if _, err := repository.CompareAndSwapResellerWithAudit(ctx, created, next, validAudit); err != nil {
		t.Fatalf("balance mutation: %v", err)
	}

	page, err := audits.ListAuditEntries(ctx, ports.AuditListFilter{
		AdminSubject: adminSubject, Action: domain.AuditActionResellerBalanceSet,
		EntityType: "reseller", EntityID: resellerID, Page: ports.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Reason != reason {
		t.Fatalf("audit entries=%+v", page.Items)
	}
}
