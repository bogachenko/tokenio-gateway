package ports

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type TelegramDeliveryAttemptStore interface {
	// StartTelegramDeliveryAttempt durably creates one started attempt before
	// any Telegram network side effect. Replaying the exact same attempt is
	// idempotent; conflicting identity returns ErrStoreConflict.
	StartTelegramDeliveryAttempt(
		context.Context,
		domain.TelegramDeliveryAttempt,
	) (domain.TelegramDeliveryAttempt, error)

	// CompleteTelegramDeliveryAttempt atomically transitions the exact persisted
	// started attempt to succeeded or failed. Terminal attempts are immutable.
	CompleteTelegramDeliveryAttempt(
		context.Context,
		domain.TelegramDeliveryAttempt,
	) (domain.TelegramDeliveryAttempt, error)

	// LoadTelegramDeliveryAttempts returns attempts for one alert ordered by
	// attempt_number ASC and limited by the caller.
	LoadTelegramDeliveryAttempts(
		context.Context,
		string,
		int,
	) ([]domain.TelegramDeliveryAttempt, error)

	// LoadStartedTelegramDeliveryAttemptsBefore returns at most limit durable
	// started attempts whose started_at is strictly before cutoff. Results are
	// ordered by started_at ASC, alert_id ASC, attempt_number ASC.
	LoadStartedTelegramDeliveryAttemptsBefore(
		context.Context,
		time.Time,
		int,
	) ([]domain.TelegramDeliveryAttempt, error)
}
