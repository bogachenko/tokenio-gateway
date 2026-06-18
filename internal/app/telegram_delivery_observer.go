package app

import (
	"fmt"
	"log"

	telegramdelivery "github.com/bogachenko/tokenio-gateway/internal/worker/telegramdelivery"
)

type TelegramDeliveryLogObserver struct {
	logger *log.Logger
}

func NewTelegramDeliveryLogObserver(
	logger *log.Logger,
) (*TelegramDeliveryLogObserver, error) {
	if logger == nil {
		return nil, fmt.Errorf("Telegram delivery logger is nil")
	}
	return &TelegramDeliveryLogObserver{logger: logger}, nil
}

func (o *TelegramDeliveryLogObserver) ObserveTelegramDeliveryCycle(
	cycle telegramdelivery.Cycle,
) {
	if cycle.Err != nil {
		o.logger.Printf(
			"Telegram delivery cycle failed: selected=%d delivered=%d failed=%d uncertain=%d skipped=%d err=%v",
			cycle.Result.Selected,
			cycle.Result.Delivered,
			cycle.Result.Failed,
			cycle.Result.Uncertain,
			cycle.Result.Skipped,
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"Telegram delivery cycle completed: selected=%d delivered=%d failed=%d uncertain=%d skipped=%d",
		cycle.Result.Selected,
		cycle.Result.Delivered,
		cycle.Result.Failed,
		cycle.Result.Uncertain,
		cycle.Result.Skipped,
	)
}
