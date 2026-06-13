package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

type AdminAuditStore struct {
	db *DB
}

var _ ports.AdminAuditStore = (*AdminAuditStore)(nil)

func NewAdminAuditStore(db *DB) (*AdminAuditStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminAuditStore{db: db}, nil
}

func (s *AdminAuditStore) ListAuditEntries(
	ctx context.Context,
	filter ports.AuditListFilter,
) (ports.Page[domain.AdminAuditEntry], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.AdminAuditEntry]{}, err
	}
	if err := validateAuditListFilter(filter); err != nil {
		return ports.Page[domain.AdminAuditEntry]{}, err
	}

	where, args := buildAuditFilter(filter)
	var result ports.Page[domain.AdminAuditEntry]

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_admin_audit_log" + where
			if err := tx.QueryRow(ctx, countSQL, args...).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + adminAuditColumns + `
FROM tokenio_admin_audit_log` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.AdminAuditEntry, 0)
			for rows.Next() {
				entry, err := scanAdminAuditEntry(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, entry)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.AdminAuditEntry]{}, err
	}
	return result, nil
}

func buildAuditFilter(filter ports.AuditListFilter) (string, []any) {
	var clauses []string
	var args []any

	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(expression, len(args)),
		)
	}

	if filter.AdminSubject != "" {
		add("admin_subject = $%d", filter.AdminSubject)
	}
	if filter.Action != "" {
		add("action = $%d", string(filter.Action))
	}
	if filter.EntityType != "" {
		add("entity_type = $%d", filter.EntityType)
	}
	if filter.EntityID != "" {
		add("entity_id = $%d", filter.EntityID)
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", filter.CreatedFrom.UTC())
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", filter.CreatedTo.UTC())
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func validateAuditListFilter(filter ports.AuditListFilter) error {
	if filter.CreatedFrom != nil &&
		!isAdminUTCTime(*filter.CreatedFrom) {
		return ports.ErrStoreContractViolation
	}
	if filter.CreatedTo != nil &&
		!isAdminUTCTime(*filter.CreatedTo) {
		return ports.ErrStoreContractViolation
	}
	if filter.CreatedFrom != nil &&
		filter.CreatedTo != nil &&
		!filter.CreatedFrom.Before(*filter.CreatedTo) {
		return ports.ErrStoreContractViolation
	}
	return nil
}
