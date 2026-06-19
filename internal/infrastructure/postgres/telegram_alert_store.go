package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const telegramAlertColumns = `
    id,
    alert_type,
    dedupe_key,
    reseller_id,
    route_id,
    message,
    status,
    error,
    created_at,
    sent_at
`

const findTelegramAlertSQL = `
SELECT
` + telegramAlertColumns + `
FROM tokenio_telegram_alerts
WHERE id = $1
`

const findTelegramAlertForUpdateSQL = `
SELECT
` + telegramAlertColumns + `
FROM tokenio_telegram_alerts
WHERE id = $1
FOR UPDATE
`

const insertTelegramAlertSQL = `
INSERT INTO tokenio_telegram_alerts (
    id,
    alert_type,
    dedupe_key,
    reseller_id,
    route_id,
    message,
    status,
    error,
    created_at,
    sent_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING
` + telegramAlertColumns

const updateTelegramAlertSQL = `
UPDATE tokenio_telegram_alerts
SET
    alert_type = $2,
    dedupe_key = $3,
    reseller_id = $4,
    route_id = $5,
    message = $6,
    status = $7,
    error = $8,
    created_at = $9,
    sent_at = $10
WHERE id = $1
RETURNING
` + telegramAlertColumns

type TelegramAlertStore struct {
	db *DB
}

var _ ports.TelegramAlertStore = (*TelegramAlertStore)(nil)

func NewTelegramAlertStore(
	db *DB,
) (*TelegramAlertStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &TelegramAlertStore{db: db}, nil
}

func (s *TelegramAlertStore) CreateOrSuppressTelegramAlert(
	ctx context.Context,
	requested domain.TelegramAlert,
	dedupeWindow time.Duration,
) (domain.TelegramAlert, error) {
	if dedupeWindow <= 0 ||
		requested.Status != domain.TelegramAlertStatusPending ||
		requested.Error != "" ||
		requested.SentAt != nil {
		return domain.TelegramAlert{},
			ports.ErrStoreContractViolation
	}
	if err := validateTelegramAlertPersistence(requested); err != nil {
		return domain.TelegramAlert{}, err
	}

	persistedInput := canonicalTelegramAlert(requested)
	var result domain.TelegramAlert

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if _, err := tx.Exec(
				ctx,
				`
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'tokenio_telegram_alert:' || $1 || ':' || $2,
        0
    )
)
`,
				persistedInput.AlertType,
				persistedInput.DedupeKey,
			); err != nil {
				return NormalizeError(err)
			}

			existing, err := scanTelegramAlert(
				tx.QueryRow(
					ctx,
					findTelegramAlertForUpdateSQL,
					persistedInput.ID,
				),
			)
			switch {
			case err == nil:
				if sameTelegramAlertIdentity(
					existing,
					persistedInput,
				) {
					result = existing
					return nil
				}
				return ports.ErrStoreConflict
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			if err := validateOperationalReferences(
				ctx,
				tx,
				persistedInput.RouteID,
				persistedInput.ResellerID,
				"",
				"",
				"",
				"",
			); err != nil {
				return err
			}

			status := domain.TelegramAlertStatusPending
			var duplicateID string
			err = tx.QueryRow(
				ctx,
				`
SELECT id
FROM tokenio_telegram_alerts
WHERE alert_type = $1
  AND dedupe_key = $2
  AND status <> 'suppressed'
  AND created_at >= $3
  AND created_at <= $4
ORDER BY created_at DESC, id ASC
LIMIT 1
`,
				persistedInput.AlertType,
				persistedInput.DedupeKey,
				persistedInput.CreatedAt.Add(-dedupeWindow),
				persistedInput.CreatedAt,
			).Scan(&duplicateID)
			switch {
			case err == nil:
				status = domain.TelegramAlertStatusSuppressed
			case errors.Is(
				normalizeRegistryReadError(err),
				ports.ErrNotFound,
			):
			default:
				return normalizeRegistryReadError(err)
			}

			persistedInput.Status = status
			value, err := scanTelegramAlert(tx.QueryRow(
				ctx,
				insertTelegramAlertSQL,
				persistedInput.ID,
				persistedInput.AlertType,
				persistedInput.DedupeKey,
				nullIfEmpty(persistedInput.ResellerID),
				nullIfEmpty(persistedInput.RouteID),
				persistedInput.Message,
				string(persistedInput.Status),
				nil,
				persistedInput.CreatedAt,
				nil,
			))
			if err != nil {
				return NormalizeError(err)
			}
			if !sameTelegramAlert(value, persistedInput) {
				return ports.ErrStoreContractViolation
			}
			result = value
			return nil
		},
	)
	if err != nil {
		return domain.TelegramAlert{}, err
	}
	return result, nil
}

func (s *TelegramAlertStore) FindTelegramAlertByID(
	ctx context.Context,
	alertID string,
) (*domain.TelegramAlert, error) {
	value, err := scanTelegramAlert(
		s.db.QueryRow(ctx, findTelegramAlertSQL, alertID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *TelegramAlertStore) ListTelegramAlerts(
	ctx context.Context,
	filter ports.TelegramAlertListFilter,
) (ports.Page[domain.TelegramAlert], error) {
	if err := validateOperationalPage(filter.Page); err != nil {
		return ports.Page[domain.TelegramAlert]{}, err
	}
	if err := validateOperationalWindow(
		filter.CreatedFrom,
		filter.CreatedTo,
	); err != nil {
		return ports.Page[domain.TelegramAlert]{}, err
	}
	if filter.Status != "" &&
		!validTelegramAlertStatus(filter.Status) {
		return ports.Page[domain.TelegramAlert]{},
			ports.ErrStoreContractViolation
	}

	where, args := buildTelegramAlertFilter(filter)
	var result ports.Page[domain.TelegramAlert]

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			if err := tx.QueryRow(
				ctx,
				"SELECT COUNT(*) FROM tokenio_telegram_alerts"+where,
				args...,
			).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			query := `
SELECT
` + telegramAlertColumns + `
FROM tokenio_telegram_alerts` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, query, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.TelegramAlert, 0)
			for rows.Next() {
				value, err := scanTelegramAlert(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, value)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.TelegramAlert]{}, err
	}
	return result, nil
}

func (s *TelegramAlertStore) ResetActiveTelegramAlertsForDedupeKey(
	ctx context.Context,
	alertType string,
	dedupeKey string,
) (int, error) {
	if strings.TrimSpace(alertType) == "" ||
		strings.TrimSpace(dedupeKey) == "" {
		return 0, ports.ErrStoreContractViolation
	}

	var affected int64
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if _, err := tx.Exec(
				ctx,
				`
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'tokenio_telegram_alert:' || $1 || ':' || $2,
        0
    )
)
`,
				alertType,
				dedupeKey,
			); err != nil {
				return NormalizeError(err)
			}

			tag, err := tx.Exec(
				ctx,
				`
UPDATE tokenio_telegram_alerts
SET
    status = 'suppressed',
    error = '',
    sent_at = NULL
WHERE alert_type = $1
  AND dedupe_key = $2
  AND status IN ('pending', 'failed')
`,
				alertType,
				dedupeKey,
			)
			if err != nil {
				return NormalizeError(err)
			}
			affected = tag.RowsAffected()
			return nil
		},
	)
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func (s *TelegramAlertStore) CompareAndSwapTelegramAlert(
	ctx context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
) (domain.TelegramAlert, error) {
	if err := validateTelegramAlertPersistence(expected); err != nil {
		return domain.TelegramAlert{}, err
	}
	if err := validateTelegramAlertPersistence(next); err != nil {
		return domain.TelegramAlert{}, err
	}
	if !sameTelegramAlertIdentity(expected, next) {
		return domain.TelegramAlert{},
			ports.ErrStoreContractViolation
	}
	if err := validateTelegramAlertTransition(
		expected,
		next,
	); err != nil {
		return domain.TelegramAlert{}, err
	}

	persistedNext := canonicalTelegramAlert(next)
	var result domain.TelegramAlert

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanTelegramAlert(
				tx.QueryRow(
					ctx,
					findTelegramAlertForUpdateSQL,
					expected.ID,
				),
			)
			if err != nil {
				return err
			}
			if !sameTelegramAlert(current, expected) {
				return ports.ErrStoreConflict
			}
			if sameTelegramAlert(current, persistedNext) {
				result = current
				return nil
			}

			value, err := scanTelegramAlert(tx.QueryRow(
				ctx,
				updateTelegramAlertSQL,
				persistedNext.ID,
				persistedNext.AlertType,
				persistedNext.DedupeKey,
				nullIfEmpty(persistedNext.ResellerID),
				nullIfEmpty(persistedNext.RouteID),
				persistedNext.Message,
				string(persistedNext.Status),
				nullIfEmpty(persistedNext.Error),
				persistedNext.CreatedAt,
				operationalTimeArg(persistedNext.SentAt),
			))
			if err != nil {
				return NormalizeError(err)
			}
			if !sameTelegramAlert(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}
			result = value
			return nil
		},
	)
	if err != nil {
		return domain.TelegramAlert{}, err
	}
	return result, nil
}

func validateTelegramAlertTransition(
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
) error {
	if sameTelegramAlert(expected, next) {
		return nil
	}

	switch expected.Status {
	case domain.TelegramAlertStatusPending:
		if next.Status == domain.TelegramAlertStatusSuppressed &&
			next.Error == "" &&
			next.SentAt == nil {
			return nil
		}
		switch next.Status {
		case domain.TelegramAlertStatusSent:
			if next.Error != "" || next.SentAt == nil {
				return ports.ErrStoreContractViolation
			}
			return nil
		case domain.TelegramAlertStatusFailed:
			if next.Error == "" || next.SentAt != nil {
				return ports.ErrStoreContractViolation
			}
			return nil
		}
	case domain.TelegramAlertStatusFailed:
		if next.Status == domain.TelegramAlertStatusSuppressed &&
			next.Error == "" &&
			next.SentAt == nil {
			return nil
		}
		if next.Status ==
			domain.TelegramAlertStatusPending &&
			next.Error == "" &&
			next.SentAt == nil {
			return nil
		}
	}
	return ports.ErrStoreContractViolation
}

func buildTelegramAlertFilter(
	filter ports.TelegramAlertListFilter,
) (string, []any) {
	var clauses []string
	var args []any
	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(expression, len(args)),
		)
	}

	if filter.AlertType != "" {
		add("alert_type = $%d", filter.AlertType)
	}
	if filter.ResellerID != "" {
		add("reseller_id = $%d", filter.ResellerID)
	}
	if filter.Status != "" {
		add("status = $%d", string(filter.Status))
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", operationalTime(*filter.CreatedFrom))
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", operationalTime(*filter.CreatedTo))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (s *TelegramAlertStore) CompareAndSwapTelegramAlertWithAudit(
	ctx context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
	audit domain.AuditContext,
) (domain.TelegramAlert, error) {
	if err := validateTelegramAlertPersistence(expected); err != nil {
		return domain.TelegramAlert{}, err
	}
	if err := validateTelegramAlertPersistence(next); err != nil {
		return domain.TelegramAlert{}, err
	}
	if !sameTelegramAlertIdentity(expected, next) {
		return domain.TelegramAlert{}, ports.ErrStoreContractViolation
	}
	if err := validateTelegramAlertTransition(expected, next); err != nil {
		return domain.TelegramAlert{}, err
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionTelegramAlertRetry,
		"telegram_alert",
		expected.ID,
		telegramAlertAuditState(expected),
		telegramAlertAuditState(next),
		audit.CreatedAt,
	); err != nil {
		return domain.TelegramAlert{}, err
	}

	persistedNext := canonicalTelegramAlert(next)
	var result domain.TelegramAlert

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanTelegramAlert(
				tx.QueryRow(ctx, findTelegramAlertForUpdateSQL, expected.ID),
			)
			if err != nil {
				return err
			}
			if !sameTelegramAlert(current, expected) {
				return ports.ErrStoreConflict
			}
			if err := insertAdminAudit(ctx, tx, audit); err != nil {
				return err
			}

			value, err := scanTelegramAlert(tx.QueryRow(
				ctx,
				updateTelegramAlertSQL,
				persistedNext.ID,
				persistedNext.AlertType,
				persistedNext.DedupeKey,
				nullIfEmpty(persistedNext.ResellerID),
				nullIfEmpty(persistedNext.RouteID),
				persistedNext.Message,
				string(persistedNext.Status),
				nullIfEmpty(persistedNext.Error),
				persistedNext.CreatedAt,
				operationalTimeArg(persistedNext.SentAt),
			))
			if err != nil {
				return NormalizeError(err)
			}
			if !sameTelegramAlert(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}
			result = value
			return nil
		},
	)
	if err != nil {
		return domain.TelegramAlert{}, err
	}
	return result, nil
}

func telegramAlertAuditState(value domain.TelegramAlert) domain.AuditState {
	return domain.AuditState{
		"id":          value.ID,
		"alert_type":  value.AlertType,
		"dedupe_key":  value.DedupeKey,
		"reseller_id": value.ResellerID,
		"route_id":    value.RouteID,
		"message":     value.Message,
		"status":      value.Status,
		"error":       value.Error,
		"created_at":  value.CreatedAt.UTC(),
		"sent_at":     adminCanonicalTimePointer(value.SentAt),
	}
}
