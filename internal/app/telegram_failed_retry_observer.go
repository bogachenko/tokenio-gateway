package app

import (
	"fmt"
	"log"

	telegramfailedretry "github.com/bogachenko/tokenio-gateway/internal/worker/telegramfailedretry"
)

type TelegramFailedRetryLogObserver struct {
	logger *log.Logger
}

func NewTelegramFailedRetryLogObserver(
	logger *log.Logger,
) (*TelegramFailedRetryLogObserver, error) {
	if logger == nil {
		return nil, fmt.Errorf("Telegram failed-retry logger is nil")
	}
	return &TelegramFailedRetryLogObserver{logger: logger}, nil
}

func (o *TelegramFailedRetryLogObserver) ObserveTelegramFailedRetryCycle(
	cycle telegramfailedretry.Cycle,
) {
	if cycle.Err != nil {
		o.logger.Printf(
			"Telegram failed-retry cycle failed: selected=%d retried=%d sent=%d err=%v",
			cycle.Result.Selected,
			cycle.Result.Retried,
			cycle.Result.Sent,
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"Telegram failed-retry cycle completed: selected=%d retried=%d sent=%d",
		cycle.Result.Selected,
		cycle.Result.Retried,
		cycle.Result.Sent,
	)
}
