package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const adminAPIKeyProvisioningColumns = `
    p.id,
    p.external_billing_user_id,
    p.user_id,
    p.api_key_id,
    k.key_prefix,
    p.result_type,
    p.status,
    p.source_reference_hash,
    p.created_at,
    p.expires_at,
    p.delivered_at,
    p.expired_at
`

type AdminAPIKeyProvisioningRepository struct {
	db *DB
}

var _ ports.AdminAPIKeyProvisioningRepository = (*AdminAPIKeyProvisioningRepository)(nil)

func NewAdminAPIKeyProvisioningRepository(
	db *DB,
) (*AdminAPIKeyProvisioningRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminAPIKeyProvisioningRepository{
		db: db,
	}, nil
}

func (r *AdminAPIKeyProvisioningRepository) ListAPIKeyProvisionings(
	ctx context.Context,
	filter ports.APIKeyProvisioningListFilter,
) (ports.Page[ports.APIKeyProvisioningAdminRecord], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[ports.APIKeyProvisioningAdminRecord]{}, err
	}
	if err := validateAdminAPIKeyProvisioningFilter(
		filter,
	); err != nil {
		return ports.Page[ports.APIKeyProvisioningAdminRecord]{}, err
	}

	where, args :=
		buildAdminAPIKeyProvisioningFilter(filter)
	var result ports.Page[ports.APIKeyProvisioningAdminRecord]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := `
SELECT COUNT(*)
FROM tokenio_api_key_provisionings p
JOIN tokenio_api_keys k ON k.id = p.api_key_id` +
				where
			if err := tx.QueryRow(
				ctx,
				countSQL,
				args...,
			).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(
				listArgs,
				filter.Page.Limit,
			)
			offsetPosition := len(listArgs) + 1
			listArgs = append(
				listArgs,
				filter.Page.Offset,
			)

			listSQL := `
SELECT
` + adminAPIKeyProvisioningColumns + `
FROM tokenio_api_key_provisionings p
JOIN tokenio_api_keys k ON k.id = p.api_key_id` +
				where + fmt.Sprintf(`
ORDER BY p.created_at DESC, p.id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(
				ctx,
				listSQL,
				listArgs...,
			)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make(
				[]ports.APIKeyProvisioningAdminRecord,
				0,
			)
			for rows.Next() {
				record, err :=
					scanAdminAPIKeyProvisioning(rows)
				if err != nil {
					return err
				}
				result.Items = append(
					result.Items,
					record,
				)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[ports.APIKeyProvisioningAdminRecord]{}, err
	}
	return result, nil
}

type adminAPIKeyProvisioningScanner interface {
	Scan(dest ...any) error
}

func scanAdminAPIKeyProvisioning(
	row adminAPIKeyProvisioningScanner,
) (ports.APIKeyProvisioningAdminRecord, error) {
	var value ports.APIKeyProvisioningAdminRecord
	var resultType string
	var status string
	var expiresAt pgtype.Timestamptz
	var deliveredAt pgtype.Timestamptz
	var expiredAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.ExternalBillingUserID,
		&value.UserID,
		&value.APIKeyID,
		&value.KeyPrefix,
		&resultType,
		&status,
		&value.SourceReferenceHash,
		&value.CreatedAt,
		&expiresAt,
		&deliveredAt,
		&expiredAt,
	); err != nil {
		return ports.APIKeyProvisioningAdminRecord{},
			normalizeRegistryReadError(err)
	}

	value.ResultType =
		domain.APIKeyProvisioningResultType(resultType)
	value.Status =
		domain.APIKeyProvisioningStatus(status)
	value.CreatedAt = provisioningTime(
		value.CreatedAt,
	)
	value.ExpiresAt = optionalTime(expiresAt)
	value.DeliveredAt = optionalTime(deliveredAt)
	value.ExpiredAt = optionalTime(expiredAt)

	if err := validateAdminAPIKeyProvisioningRecord(
		value,
	); err != nil {
		return ports.APIKeyProvisioningAdminRecord{},
			err
	}
	return value, nil
}

func validateAdminAPIKeyProvisioningFilter(
	filter ports.APIKeyProvisioningListFilter,
) error {
	for _, value := range []string{
		filter.ProvisioningID,
		filter.ExternalBillingUserID,
		filter.UserID,
		filter.APIKeyID,
	} {
		if value != "" &&
			value != strings.TrimSpace(value) {
			return ports.ErrStoreContractViolation
		}
	}

	if filter.Status != "" &&
		!validAdminProvisioningStatus(filter.Status) {
		return ports.ErrStoreContractViolation
	}
	if filter.ResultType != "" &&
		!validAdminProvisioningResultType(
			filter.ResultType,
		) {
		return ports.ErrStoreContractViolation
	}
	if filter.CreatedFrom != nil &&
		!isProvisioningUTCTime(
			*filter.CreatedFrom,
		) {
		return ports.ErrStoreContractViolation
	}
	if filter.CreatedTo != nil &&
		!isProvisioningUTCTime(
			*filter.CreatedTo,
		) {
		return ports.ErrStoreContractViolation
	}
	if filter.CreatedFrom != nil &&
		filter.CreatedTo != nil &&
		!filter.CreatedFrom.Before(
			*filter.CreatedTo,
		) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func buildAdminAPIKeyProvisioningFilter(
	filter ports.APIKeyProvisioningListFilter,
) (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	add := func(column string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(
				"%s = $%d",
				column,
				len(args),
			),
		)
	}

	if filter.ProvisioningID != "" {
		add("p.id", filter.ProvisioningID)
	}
	if filter.ExternalBillingUserID != "" {
		add(
			"p.external_billing_user_id",
			filter.ExternalBillingUserID,
		)
	}
	if filter.UserID != "" {
		add("p.user_id", filter.UserID)
	}
	if filter.APIKeyID != "" {
		add("p.api_key_id", filter.APIKeyID)
	}
	if filter.Status != "" {
		add("p.status", string(filter.Status))
	}
	if filter.ResultType != "" {
		add(
			"p.result_type",
			string(filter.ResultType),
		)
	}
	if filter.CreatedFrom != nil {
		args = append(
			args,
			provisioningTime(
				*filter.CreatedFrom,
			),
		)
		clauses = append(
			clauses,
			fmt.Sprintf(
				"p.created_at >= $%d",
				len(args),
			),
		)
	}
	if filter.CreatedTo != nil {
		args = append(
			args,
			provisioningTime(*filter.CreatedTo),
		)
		clauses = append(
			clauses,
			fmt.Sprintf(
				"p.created_at < $%d",
				len(args),
			),
		)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " +
			strings.Join(clauses, " AND "),
		args
}

func validateAdminAPIKeyProvisioningRecord(
	value ports.APIKeyProvisioningAdminRecord,
) error {
	if value.ID == "" ||
		value.ExternalBillingUserID == "" ||
		value.UserID == "" ||
		value.APIKeyID == "" ||
		value.KeyPrefix == "" ||
		!validSHA256Hex(
			value.SourceReferenceHash,
		) ||
		!isProvisioningUTCTime(
			value.CreatedAt,
		) ||
		value.ExpiresAt != nil &&
			!isProvisioningUTCTime(
				*value.ExpiresAt,
			) ||
		value.DeliveredAt != nil &&
			!isProvisioningUTCTime(
				*value.DeliveredAt,
			) ||
		value.ExpiredAt != nil &&
			!isProvisioningUTCTime(
				*value.ExpiredAt,
			) {
		return ports.ErrStoreContractViolation
	}

	switch value.ResultType {
	case domain.APIKeyProvisioningResultTypeKeyCreated:
		if value.ExpiresAt == nil {
			return ports.ErrStoreContractViolation
		}
	case domain.APIKeyProvisioningResultTypeAlreadyProvisioned:
		if value.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
			value.ExpiresAt != nil {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}

	switch value.Status {
	case domain.APIKeyProvisioningStatusPendingDelivery:
		if value.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			value.ExpiresAt == nil ||
			!value.ExpiresAt.After(
				value.CreatedAt,
			) ||
			value.DeliveredAt != nil ||
			value.ExpiredAt != nil {
			return ports.ErrStoreContractViolation
		}

	case domain.APIKeyProvisioningStatusDelivered:
		if value.DeliveredAt == nil ||
			value.DeliveredAt.Before(
				value.CreatedAt,
			) ||
			value.ExpiredAt != nil {
			return ports.ErrStoreContractViolation
		}

	case domain.APIKeyProvisioningStatusExpired:
		if value.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			value.ExpiresAt == nil ||
			value.ExpiredAt == nil ||
			value.ExpiredAt.Before(
				*value.ExpiresAt,
			) ||
			value.DeliveredAt != nil {
			return ports.ErrStoreContractViolation
		}

	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validAdminProvisioningStatus(
	value domain.APIKeyProvisioningStatus,
) bool {
	switch value {
	case domain.APIKeyProvisioningStatusPendingDelivery,
		domain.APIKeyProvisioningStatusDelivered,
		domain.APIKeyProvisioningStatusExpired:
		return true
	default:
		return false
	}
}

func validAdminProvisioningResultType(
	value domain.APIKeyProvisioningResultType,
) bool {
	switch value {
	case domain.APIKeyProvisioningResultTypeKeyCreated,
		domain.APIKeyProvisioningResultTypeAlreadyProvisioned:
		return true
	default:
		return false
	}
}

func validSHA256Hex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil &&
		len(decoded) == sha256.Size
}
