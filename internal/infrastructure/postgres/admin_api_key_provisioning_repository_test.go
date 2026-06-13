package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5/pgtype"
)

type adminProvisioningScanFunc func(...any) error

func (f adminProvisioningScanFunc) Scan(
	destinations ...any,
) error {
	return f(destinations...)
}

func TestAdminAPIKeyProvisioningProjectionContainsOnlySafeColumns(
	t *testing.T,
) {
	lower := strings.ToLower(
		adminAPIKeyProvisioningColumns,
	)
	for _, forbidden := range []string{
		"encrypted_raw_key",
		"encryption_nonce",
		"encryption_key_version",
		"key_hash",
		"idempotency_key",
		"delivery_attempts",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf(
				"safe projection contains %q: %s",
				forbidden,
				adminAPIKeyProvisioningColumns,
			)
		}
	}

	for _, required := range []string{
		"p.id",
		"p.external_billing_user_id",
		"p.user_id",
		"p.api_key_id",
		"k.key_prefix",
		"p.source_reference_hash",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf(
				"safe projection is missing %q",
				required,
			)
		}
	}
}

func TestScanAdminAPIKeyProvisioning(
	t *testing.T,
) {
	createdAt := time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	expiresAt := createdAt.Add(24 * time.Hour)
	sourceHash := sha256.Sum256(
		[]byte("payment-1"),
	)

	record, err := scanAdminAPIKeyProvisioning(
		adminProvisioningScanFunc(
			func(dest ...any) error {
				*dest[0].(*string) = "prov_1"
				*dest[1].(*string) = "billing_1"
				*dest[2].(*string) = "usr_1"
				*dest[3].(*string) = "ak_1"
				*dest[4].(*string) =
					"sk_live_abcd..."
				*dest[5].(*string) = "key_created"
				*dest[6].(*string) =
					"pending_delivery"
				*dest[7].(*string) =
					hex.EncodeToString(sourceHash[:])
				*dest[8].(*time.Time) = createdAt
				*dest[9].(*pgtype.Timestamptz) =
					pgtype.Timestamptz{
						Time:  expiresAt,
						Valid: true,
					}
				return nil
			},
		),
	)
	if err != nil {
		t.Fatalf(
			"scanAdminAPIKeyProvisioning: %v",
			err,
		)
	}
	if record.ID != "prov_1" ||
		record.KeyPrefix != "sk_live_abcd..." ||
		record.Status !=
			domain.APIKeyProvisioningStatusPendingDelivery ||
		record.ExpiresAt == nil ||
		!record.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("record = %+v", record)
	}
}

func TestAdminAPIKeyProvisioningFilterBuilder(
	t *testing.T,
) {
	from := time.Date(
		2026,
		time.June,
		1,
		0,
		0,
		0,
		0,
		time.UTC,
	)
	to := from.Add(24 * time.Hour)

	where, args :=
		buildAdminAPIKeyProvisioningFilter(
			ports.APIKeyProvisioningListFilter{
				ExternalBillingUserID: "billing_1",
				UserID:                "usr_1",
				APIKeyID:              "ak_1",
				Status:                domain.APIKeyProvisioningStatusDelivered,
				ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
				CreatedFrom:           &from,
				CreatedTo:             &to,
			},
		)

	expected := " WHERE " +
		"p.external_billing_user_id = $1 AND " +
		"p.user_id = $2 AND " +
		"p.api_key_id = $3 AND " +
		"p.status = $4 AND " +
		"p.result_type = $5 AND " +
		"p.created_at >= $6 AND " +
		"p.created_at < $7"
	if where != expected {
		t.Fatalf(
			"where = %q, want %q",
			where,
			expected,
		)
	}
	if len(args) != 7 ||
		args[0] != "billing_1" ||
		args[1] != "usr_1" ||
		args[2] != "ak_1" ||
		args[3] != "delivered" ||
		args[4] != "key_created" {
		t.Fatalf("args = %#v", args)
	}
}

func TestValidateAdminAPIKeyProvisioningFilter(
	t *testing.T,
) {
	from := time.Date(
		2026,
		time.June,
		2,
		0,
		0,
		0,
		0,
		time.UTC,
	)
	to := from.Add(-time.Hour)

	tests := []ports.APIKeyProvisioningListFilter{
		{
			UserID: " usr_1",
		},
		{
			Status: domain.APIKeyProvisioningStatus(
				"unknown",
			),
		},
		{
			ResultType: domain.APIKeyProvisioningResultType(
				"unknown",
			),
		},
		{
			CreatedFrom: &from,
			CreatedTo:   &to,
		},
	}

	for _, filter := range tests {
		if err :=
			validateAdminAPIKeyProvisioningFilter(
				filter,
			); !errors.Is(
			err,
			ports.ErrStoreContractViolation,
		) {
			t.Fatalf(
				"filter=%+v error=%v",
				filter,
				err,
			)
		}
	}
}

func TestNewAdminAPIKeyProvisioningRepositoryRejectsNilDB(
	t *testing.T,
) {
	repository, err :=
		NewAdminAPIKeyProvisioningRepository(nil)
	if repository != nil ||
		!errors.Is(
			err,
			ErrInvalidDatabaseConfig,
		) {
		t.Fatalf(
			"repository=%v error=%v",
			repository,
			err,
		)
	}
}

func TestScanAdminAPIKeyProvisioningNormalizesReadError(
	t *testing.T,
) {
	_, err := scanAdminAPIKeyProvisioning(
		adminProvisioningScanFunc(
			func(...any) error {
				return context.Canceled
			},
		),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
