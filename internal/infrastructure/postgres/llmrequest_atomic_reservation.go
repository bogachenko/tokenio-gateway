package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

type LLMRequestAtomicReservation struct {
	db    *DB
	clock ports.Clock
}

var _ llmrequest.AtomicReservation = (*LLMRequestAtomicReservation)(nil)

func NewLLMRequestAtomicReservation(
	db *DB,
	clock ports.Clock,
) (*LLMRequestAtomicReservation, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: nil atomic reservation clock",
			llmrequest.ErrDependencyRequired,
		)
	}
	return &LLMRequestAtomicReservation{
		db:    db,
		clock: clock,
	}, nil
}

func (s *LLMRequestAtomicReservation) Reserve(
	ctx context.Context,
	input llmrequest.ReservationInput,
) (llmrequest.ReservationResult, error) {
	if s == nil || s.db == nil || s.db.pool == nil || s.clock == nil {
		return llmrequest.ReservationResult{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return llmrequest.ReservationResult{}, fmt.Errorf(
			"%w: nil context",
			ports.ErrStoreContractViolation,
		)
	}
	if err := validateLLMRequestAtomicReservationInput(input); err != nil {
		return llmrequest.ReservationResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() {
		return llmrequest.ReservationResult{}, fmt.Errorf(
			"%w: zero atomic reservation clock",
			ports.ErrStoreContractViolation,
		)
	}
	now = now.UTC().Truncate(time.Microsecond)
	record := llmRequestReservedUsageRecord(input, now)

	var result llmrequest.ReservationResult
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			if _, err := tx.Exec(
				ctx,
				`
SELECT pg_advisory_xact_lock(
    hashtextextended('tokenio_usage_local_request:' || $1, 0)
)
`,
				input.LocalRequestID,
			); err != nil {
				return NormalizeError(err)
			}

			var lockedUserID string
			if err := tx.QueryRow(
				ctx,
				`
SELECT id
FROM tokenio_users
WHERE id = $1
FOR UPDATE
`,
				input.Principal.UserID,
			).Scan(&lockedUserID); err != nil {
				return normalizeRegistryReadError(err)
			}
			if lockedUserID != input.Principal.UserID {
				return ports.ErrStoreContractViolation
			}

			_, err := scanUsageRecord(
				tx.QueryRow(
					ctx,
					findPricingFailedUsageSQL,
					input.Principal.UserID,
				),
			)
			switch {
			case err == nil:
				return llmrequest.ErrUnresolvedUsage
			case isStoreNotFound(err):
			default:
				return err
			}

			existing, err := scanUsageRecord(
				tx.QueryRow(
					ctx,
					findUsageByLocalRequestIDSQL,
					input.LocalRequestID,
				),
			)
			switch {
			case err == nil:
				if !sameLLMRequestAtomicReservation(existing, input) {
					return llmrequest.ErrLocalRequestConflict
				}
				if existing.Status != domain.UsageStatusReserved {
					return classifyLLMRequestReservationStatus(
						existing.Status,
					)
				}
				result = llmrequest.ReservationResult{
					Disposition: llmrequest.
						ReservationDispositionAlreadyReserved,
					Usage: existing,
				}
				return nil
			case isStoreNotFound(err):
			default:
				return err
			}

			idempotencyKey := llmRequestReservationIdempotencyKey(
				input.IdempotencyKey,
			)
			if idempotencyKey != "" {
				existing, err = scanUsageRecord(
					tx.QueryRow(
						ctx,
						findUsageByIdempotencySQL,
						input.Principal.UserID,
						string(input.EndpointKind),
						idempotencyKey,
					),
				)
				switch {
				case err == nil:
					return classifyLLMRequestReservationStatus(
						existing.Status,
					)
				case isStoreNotFound(err):
				default:
					return err
				}
			}

			persistedReseller, err := scanReseller(tx.QueryRow(
				ctx,
				findResellerBalanceForUpdateSQL,
				input.Reseller.ID,
			))
			if err != nil {
				return err
			}
			if persistedReseller.ID != input.Reseller.ID ||
				persistedReseller.ID != input.Route.ResellerID ||
				persistedReseller.ProviderType !=
					input.Reseller.ProviderType ||
				persistedReseller.ProviderType !=
					input.Route.ProviderType {
				return ports.ErrStoreContractViolation
			}
			if !domain.CanReserveResellerBalance(
				persistedReseller,
				input.EstimatedUpstreamCostCents,
			) {
				return llmrequest.ErrResellerReserveUnavailable
			}

			expectedReseller := persistedReseller
			if input.EstimatedUpstreamCostCents > 0 {
				expectedReseller.ReservedCents +=
					input.EstimatedUpstreamCostCents
				if now.After(expectedReseller.UpdatedAt) {
					expectedReseller.UpdatedAt = now
				}

				persistedReseller, err = scanReseller(tx.QueryRow(
					ctx,
					updateResellerBalanceReserveSQL,
					input.Reseller.ID,
					expectedReseller.ReservedCents,
					now,
				))
				if err != nil {
					return err
				}
				if !sameResellerBalanceSnapshot(
					persistedReseller,
					expectedReseller,
				) {
					return ports.ErrStoreContractViolation
				}
			}

			if _, err := tx.Exec(
				ctx,
				insertUsageRecordSQL,
				usageRecordNamedArgs(record),
			); err != nil {
				return NormalizeError(err)
			}

			resellerCopy := persistedReseller
			result = llmrequest.ReservationResult{
				Disposition: llmrequest.ReservationDispositionCreated,
				Usage:       record,
				Reseller:    &resellerCopy,
			}
			return nil
		},
	)
	if err != nil {
		return llmrequest.ReservationResult{}, err
	}
	return result, nil
}

func validateLLMRequestAtomicReservationInput(
	input llmrequest.ReservationInput,
) error {
	if !validLLMRequestAtomicReservationIdentifier(
		input.LocalRequestID,
	) ||
		!validLLMRequestAtomicReservationText(
			input.Principal.UserID,
		) ||
		!validLLMRequestAtomicReservationText(
			input.Principal.APIKeyID,
		) ||
		!validLLMRequestAtomicReservationText(
			input.Principal.BillingSubjectUserID,
		) ||
		input.APIFamily == "" ||
		input.EndpointKind == "" ||
		!validLLMRequestAtomicReservationText(input.ClientModel) ||
		!validLLMRequestAtomicReservationText(input.BillingModel) ||
		!validLLMRequestAtomicReservationText(input.Route.ID) ||
		!validLLMRequestAtomicReservationText(input.Reseller.ID) ||
		input.Route.ResellerID != input.Reseller.ID ||
		input.Route.ProviderType == "" ||
		input.Route.ProviderType != input.Reseller.ProviderType ||
		input.Route.APIFamily != input.APIFamily ||
		input.Route.EndpointKind != input.EndpointKind ||
		input.Route.ClientModel != input.ClientModel ||
		!input.Route.Enabled ||
		!input.Reseller.Enabled ||
		input.EstimatedClientAmountCents < 0 ||
		input.EstimatedUpstreamCostCents < 0 ||
		input.Currency != "RUB" ||
		!nonNegativeLLMRequestAtomicReservationUsage(
			input.EstimatedUsage,
		) {
		return ports.ErrStoreContractViolation
	}
	if input.IdempotencyKey != nil &&
		!validLLMRequestAtomicReservationText(
			*input.IdempotencyKey,
		) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func llmRequestReservedUsageRecord(
	input llmrequest.ReservationInput,
	reservedAt time.Time,
) domain.UsageRecord {
	reservedAtCopy := reservedAt
	return domain.UsageRecord{
		LocalRequestID: input.LocalRequestID,
		IdempotencyKey: llmRequestReservationIdempotencyKey(
			input.IdempotencyKey,
		),
		UserID:             input.Principal.UserID,
		APIKeyID:           input.Principal.APIKeyID,
		APIFamily:          input.APIFamily,
		EndpointKind:       input.EndpointKind,
		ClientModel:        input.ClientModel,
		BillingModel:       input.BillingModel,
		SelectedRouteID:    input.Route.ID,
		SelectedResellerID: input.Reseller.ID,
		ProviderType:       input.Route.ProviderType,
		ProviderModel:      input.Route.ProviderModel,
		EstimatedUsage:     input.EstimatedUsage,
		EstimatedClientAmountCents: input.
			EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: input.
			EstimatedUpstreamCostCents,
		Currency:          input.Currency,
		UsageCompleteness: "missing",
		Status:            domain.UsageStatusReserved,
		CreatedAt:         reservedAt,
		ReservedAt:        &reservedAtCopy,
		UpdatedAt:         reservedAt,
	}
}

func sameLLMRequestAtomicReservation(
	existing domain.UsageRecord,
	input llmrequest.ReservationInput,
) bool {
	return existing.LocalRequestID == input.LocalRequestID &&
		existing.IdempotencyKey ==
			llmRequestReservationIdempotencyKey(
				input.IdempotencyKey,
			) &&
		existing.UserID == input.Principal.UserID &&
		existing.APIKeyID == input.Principal.APIKeyID &&
		existing.APIFamily == input.APIFamily &&
		existing.EndpointKind == input.EndpointKind &&
		existing.ClientModel == input.ClientModel &&
		existing.BillingModel == input.BillingModel &&
		existing.SelectedRouteID == input.Route.ID &&
		existing.SelectedResellerID == input.Reseller.ID &&
		existing.ProviderType == input.Route.ProviderType &&
		existing.ProviderModel == input.Route.ProviderModel &&
		existing.EstimatedUsage == input.EstimatedUsage &&
		existing.EstimatedClientAmountCents ==
			input.EstimatedClientAmountCents &&
		existing.EstimatedUpstreamCostCents ==
			input.EstimatedUpstreamCostCents &&
		existing.Currency == input.Currency
}

func classifyLLMRequestReservationStatus(
	status domain.UsageStatus,
) error {
	switch status {
	case domain.UsageStatusReserved:
		return llmrequest.ErrRequestInProgress
	case domain.UsageStatusBillable,
		domain.UsageStatusPartiallyCharged,
		domain.UsageStatusCharged:
		return llmrequest.ErrIdempotencyReplayNotAvailable
	case domain.UsageStatusReleased,
		domain.UsageStatusFailed:
		return llmrequest.ErrIdempotencyKeyReused
	case domain.UsageStatusPricingFailed:
		return llmrequest.ErrUnresolvedUsage
	default:
		return ports.ErrStoreContractViolation
	}
}

func llmRequestReservationIdempotencyKey(
	value *string,
) string {
	if value == nil {
		return ""
	}
	return *value
}

func validLLMRequestAtomicReservationIdentifier(
	value string,
) bool {
	return strings.HasPrefix(value, "llmreq_") &&
		len(value) > len("llmreq_") &&
		validLLMRequestAtomicReservationText(value)
}

func validLLMRequestAtomicReservationText(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}
	return true
}

func nonNegativeLLMRequestAtomicReservationUsage(
	value domain.TokenUsage,
) bool {
	return value.InputTokens >= 0 &&
		value.CachedInputTokens >= 0 &&
		value.OutputTokens >= 0 &&
		value.ReasoningTokens >= 0 &&
		value.ImageInputTokens >= 0 &&
		value.AudioInputTokens >= 0 &&
		value.AudioOutputTokens >= 0 &&
		value.FileInputTokens >= 0 &&
		value.VideoInputTokens >= 0 &&
		value.ImageGenerationUnits >= 0
}

func isStoreNotFound(err error) bool {
	return err == ports.ErrNotFound
}
