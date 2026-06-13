package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewAdminResellerRepositoryRejectsNilDB(t *testing.T) {
	_, err := NewAdminResellerRepository(nil)
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("error = %v, want invalid database config", err)
	}
}

func TestAdminResellerStateContainsOnlyReferenceToSecret(t *testing.T) {
	value := adminResellerTestRecord()
	state := adminResellerState(value)

	if state["api_key_env"] != value.APIKeyEnv {
		t.Fatalf("api_key_env = %#v", state["api_key_env"])
	}
	if auditStateContainsSecret(state) {
		t.Fatal("reseller audit state classified as secret")
	}
}

func TestValidateAdminResellerMutation(t *testing.T) {
	base := adminResellerTestRecord()

	tests := []struct {
		name   string
		action domain.AuditAction
		next   func(domain.Reseller) domain.Reseller
	}{
		{
			name:   "update",
			action: domain.AuditActionResellerUpdate,
			next: func(value domain.Reseller) domain.Reseller {
				value.Name = "Updated"
				value.UpdatedAt = value.UpdatedAt.Add(time.Second)
				return value
			},
		},
		{
			name:   "disable",
			action: domain.AuditActionResellerDisable,
			next: func(value domain.Reseller) domain.Reseller {
				at := value.UpdatedAt.Add(time.Second)
				value.Enabled = false
				value.DisabledAt = &at
				value.UpdatedAt = at
				return value
			},
		},
		{
			name:   "balance adjust",
			action: domain.AuditActionResellerBalanceAdjust,
			next: func(value domain.Reseller) domain.Reseller {
				value.BalanceCents += 100
				value.UpdatedAt = value.UpdatedAt.Add(time.Second)
				return value
			},
		},
		{
			name:   "balance set",
			action: domain.AuditActionResellerBalanceSet,
			next: func(value domain.Reseller) domain.Reseller {
				value.BalanceCents = -100
				value.UpdatedAt = value.UpdatedAt.Add(time.Second)
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := test.next(base)
			if err := validateAdminResellerMutation(
				base,
				next,
				test.action,
			); err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func TestValidateAdminResellerMutationRejectsWrongActionShape(
	t *testing.T,
) {
	base := adminResellerTestRecord()
	next := base
	next.BalanceCents++
	next.UpdatedAt = next.UpdatedAt.Add(time.Second)

	if err := validateAdminResellerMutation(
		base,
		next,
		domain.AuditActionResellerUpdate,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestAdminResellerPostgresTimestampPrecision(t *testing.T) {
	value := time.Unix(10, 123456789).UTC()
	got := postgresAdminTime(value)
	want := time.Unix(10, 123456000).UTC()
	if !got.Equal(want) {
		t.Fatalf("postgresAdminTime = %s, want %s", got, want)
	}
}

func adminResellerTestRecord() domain.Reseller {
	now := time.Unix(100, 123456000).UTC()
	return domain.Reseller{
		ID:                  "reseller-1",
		Name:                "Primary",
		ProviderType:        domain.ProviderOpenAI,
		BaseURL:             "https://example.test",
		APIKeyEnv:           "OPENAI_PRIMARY_API_KEY",
		Enabled:             true,
		BalanceCents:        1000,
		ReservedCents:       0,
		MinimumBalanceCents: 100,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func TestAdminResellerApplicationAndPersistedAuditPrecision(t *testing.T) {
	nanosecondTime := time.Unix(100, 123456789).UTC()
	value := adminResellerTestRecord()
	value.CreatedAt = nanosecondTime
	value.UpdatedAt = nanosecondTime

	applicationState := adminResellerApplicationState(value)
	persistedState := adminResellerState(value)

	if auditStateEqual(applicationState, persistedState) {
		t.Fatal("application and persisted timestamp precision must differ")
	}

	audit := domain.AuditContext{
		ID:           "audit-1",
		AdminSubject: "admin_token",
		Action:       domain.AuditActionResellerCreate,
		EntityType:   "reseller",
		EntityID:     value.ID,
		BeforeState:  domain.AuditState{},
		AfterState:   applicationState,
		RequestID:    "admreq-1",
		CreatedAt:    nanosecondTime,
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionResellerCreate,
		"reseller",
		value.ID,
		domain.AuditState{},
		applicationState,
		nanosecondTime,
	); err != nil {
		t.Fatalf("application audit validation: %v", err)
	}

	canonicalAudit := canonicalResellerAudit(
		audit,
		domain.AuditState{},
		persistedState,
		nanosecondTime,
	)
	if !canonicalAudit.CreatedAt.Equal(postgresAdminTime(nanosecondTime)) {
		t.Fatalf("canonical audit time = %s", canonicalAudit.CreatedAt)
	}
	if !auditStateEqual(canonicalAudit.AfterState, persistedState) {
		t.Fatal("persisted audit state is not canonical")
	}
}
