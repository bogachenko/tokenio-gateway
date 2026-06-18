package app

import (
	"errors"
	"log"

	telegramstaleattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/telegramstaleattemptrecovery"
)

var ErrInvalidTelegramStaleAttemptRecoveryObserver = errors.New(
	"invalid Telegram stale-attempt recovery observer",
)

type TelegramStaleAttemptRecoveryLogObserver struct {
	logger *log.Logger
}

func NewTelegramStaleAttemptRecoveryLogObserver(
	logger *log.Logger,
) (*TelegramStaleAttemptRecoveryLogObserver, error) {
	if logger == nil {
		return nil, ErrInvalidTelegramStaleAttemptRecoveryObserver
	}
	return &TelegramStaleAttemptRecoveryLogObserver{
		logger: logger,
	}, nil
}

func (o *TelegramStaleAttemptRecoveryLogObserver) ObserveTelegramStaleAttemptRecoveryCycle(
	cycle telegramstaleattemptrecovery.Cycle,
) {
	if o == nil || o.logger == nil {
		return
	}
	if cycle.Err != nil {
		o.logger.Printf(
			"Telegram stale-attempt recovery cycle failed loaded=%d completed=%d conflicts=%d uncertain=%d error_type=%T",
			cycle.Result.Loaded,
			cycle.Result.Completed,
			cycle.Result.Conflicts,
			cycle.Result.Uncertain,
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"Telegram stale-attempt recovery cycle completed loaded=%d completed=%d conflicts=%d uncertain=%d",
		cycle.Result.Loaded,
		cycle.Result.Completed,
		cycle.Result.Conflicts,
		cycle.Result.Uncertain,
	)
}

var _ telegramstaleattemptrecovery.Observer = (*TelegramStaleAttemptRecoveryLogObserver)(nil)
