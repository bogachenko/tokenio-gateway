package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminAPIKeyStateDoesNotContainHash(t *testing.T) {
	state := adminAPIKeyState(domain.APIKeyRecord{
		ID:        "key-1",
		UserID:    "user-1",
		Name:      "Laptop",
		KeyHash:   strings.Repeat("a", 64),
		KeyPrefix: "sk_live_abcd...",
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
	})
	if _, exists := state["key_hash"]; exists {
		t.Fatal("key_hash leaked into audit state")
	}
	if auditStateContainsSecret(state) {
		t.Fatal("canonical API-key audit state classified as secret")
	}
}

func TestAuditStateRejectsSecretKeysRecursively(t *testing.T) {
	state := domain.AuditState{
		"nested": map[string]any{
			"key_hash": strings.Repeat("a", 64),
		},
	}
	if !auditStateContainsSecret(state) {
		t.Fatal("nested secret key was not detected")
	}
	if _, err := encodeAuditState(state); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestAuditStateSemanticEquality(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	left := domain.AuditState{
		"id":         "user-1",
		"enabled":    true,
		"created_at": at,
	}
	right := domain.AuditState{
		"created_at": at,
		"enabled":    true,
		"id":         "user-1",
	}
	if !auditStateEqual(left, right) {
		t.Fatal("map key order changed audit equality")
	}
}

func TestAdminRepositoryConstructorsRejectNilDB(t *testing.T) {
	constructors := []func() error{
		func() error {
			_, err := NewAdminUserRepository(nil)
			return err
		},
		func() error {
			_, err := NewAdminAPIKeyRepository(nil)
			return err
		},
		func() error {
			_, err := NewAdminAuditStore(nil)
			return err
		},
	}
	for index, constructor := range constructors {
		if err := constructor(); !errors.Is(
			err,
			ErrInvalidDatabaseConfig,
		) {
			t.Fatalf(
				"constructor %d error = %v, want invalid config",
				index,
				err,
			)
		}
	}
}
