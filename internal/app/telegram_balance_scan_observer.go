package app

import (
	"fmt"
	"log"

	telegrambalancescan "github.com/bogachenko/tokenio-gateway/internal/worker/telegrambalancescan"
)

type TelegramBalanceScanLogObserver struct {
	logger *log.Logger
}

func NewTelegramBalanceScanLogObserver(
	logger *log.Logger,
) (*TelegramBalanceScanLogObserver, error) {
	if logger == nil {
		return nil, fmt.Errorf("Telegram balance-scan logger is nil")
	}
	return &TelegramBalanceScanLogObserver{logger: logger}, nil
}

func (o *TelegramBalanceScanLogObserver) ObserveTelegramBalanceScanCycle(
	cycle telegrambalancescan.Cycle,
) {
	if cycle.Err != nil {
		o.logger.Printf(
			"Telegram balance-scan cycle failed: selected=%d checked=%d below_threshold=%d alerted=%d failed=%d skipped=%d next_offset=%d finished=%t err=%v",
			cycle.Result.Selected,
			cycle.Result.Checked,
			cycle.Result.BelowThreshold,
			cycle.Result.Alerted,
			cycle.Result.Failed,
			cycle.Result.Skipped,
			cycle.Result.NextOffset,
			cycle.Result.Finished,
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"Telegram balance-scan cycle completed: selected=%d checked=%d below_threshold=%d alerted=%d failed=%d skipped=%d next_offset=%d finished=%t",
		cycle.Result.Selected,
		cycle.Result.Checked,
		cycle.Result.BelowThreshold,
		cycle.Result.Alerted,
		cycle.Result.Failed,
		cycle.Result.Skipped,
		cycle.Result.NextOffset,
		cycle.Result.Finished,
	)
}
