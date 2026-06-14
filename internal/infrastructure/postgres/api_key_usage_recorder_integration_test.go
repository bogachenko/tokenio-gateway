package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAPIKeyUsageRecorderIntegration(
	t *testing.T,
) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip(
			"TOKENIO_TEST_DATABASE_DSN is not set",
		)
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

	suffix := strconv.FormatInt(
		time.Now().UnixNano(),
		10,
	)
	userID := "usage-user-" + suffix
	apiKeyID := "usage-key-" + suffix
	keyHash := "usage-hash-" + suffix

	base := time.Now().
		UTC().
		Truncate(time.Microsecond)
	createdAt := base.Add(-2 * time.Hour)
	firstUse := base.Add(-30 * time.Minute)
	olderUse := firstUse.Add(-time.Minute)
	newerUse := firstUse.Add(time.Minute)
	expiresAt := base.Add(time.Hour)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_keys WHERE id = $1",
			apiKeyID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id = $1",
			userID,
		)
	})

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    enabled,
    created_at,
    updated_at
)
VALUES ($1, $2, TRUE, $3, $3)
`,
		userID,
		"billing-"+suffix,
		createdAt,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_api_keys (
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    enabled,
    created_at,
    updated_at,
    expires_at
)
VALUES (
    $1,
    $2,
    'usage recorder integration',
    $3,
    'sk_test',
    TRUE,
    $4,
    $4,
    $5
)
`,
		apiKeyID,
		userID,
		keyHash,
		createdAt,
		expiresAt,
	); err != nil {
		t.Fatalf("insert API key: %v", err)
	}

	repository, err := NewAPIKeyRepository(db)
	if err != nil {
		t.Fatalf("NewAPIKeyRepository: %v", err)
	}

	if err := repository.RecordLastUsedAt(
		ctx,
		apiKeyID,
		firstUse,
	); err != nil {
		t.Fatalf("first RecordLastUsedAt: %v", err)
	}
	assertPersistedLastUsedAt(
		t,
		ctx,
		repository,
		keyHash,
		firstUse,
	)

	if err := repository.RecordLastUsedAt(
		ctx,
		apiKeyID,
		olderUse,
	); err != nil {
		t.Fatalf("older RecordLastUsedAt: %v", err)
	}
	assertPersistedLastUsedAt(
		t,
		ctx,
		repository,
		keyHash,
		firstUse,
	)

	if err := repository.RecordLastUsedAt(
		ctx,
		apiKeyID,
		newerUse,
	); err != nil {
		t.Fatalf("newer RecordLastUsedAt: %v", err)
	}
	assertPersistedLastUsedAt(
		t,
		ctx,
		repository,
		keyHash,
		newerUse,
	)

	if _, err := db.Exec(
		ctx,
		`
UPDATE tokenio_api_keys
SET enabled = FALSE
WHERE id = $1
`,
		apiKeyID,
	); err != nil {
		t.Fatalf("disable API key: %v", err)
	}

	err = repository.RecordLastUsedAt(
		ctx,
		apiKeyID,
		newerUse.Add(time.Minute),
	)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf(
			"disabled key error=%v want ErrNotFound",
			err,
		)
	}
	assertPersistedLastUsedAt(
		t,
		ctx,
		repository,
		keyHash,
		newerUse,
	)

	revokedAt := base
	if _, err := db.Exec(
		ctx,
		`
UPDATE tokenio_api_keys
SET
    enabled = TRUE,
    revoked_at = $2
WHERE id = $1
`,
		apiKeyID,
		revokedAt,
	); err != nil {
		t.Fatalf("revoke API key: %v", err)
	}

	err = repository.RecordLastUsedAt(
		ctx,
		apiKeyID,
		newerUse.Add(2*time.Minute),
	)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf(
			"revoked key error=%v want ErrNotFound",
			err,
		)
	}
	assertPersistedLastUsedAt(
		t,
		ctx,
		repository,
		keyHash,
		newerUse,
	)

	err = repository.RecordLastUsedAt(
		ctx,
		"missing-"+suffix,
		newerUse,
	)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf(
			"unknown key error=%v want ErrNotFound",
			err,
		)
	}
}

func assertPersistedLastUsedAt(
	t *testing.T,
	ctx context.Context,
	repository *APIKeyRepository,
	keyHash string,
	want time.Time,
) {
	t.Helper()

	record, err := repository.FindByHash(
		ctx,
		keyHash,
	)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if record.LastUsedAt == nil ||
		!record.LastUsedAt.Equal(want) {
		t.Fatalf(
			"last_used_at=%v want=%v",
			record.LastUsedAt,
			want,
		)
	}
}
