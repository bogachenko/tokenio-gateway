package app

import (
	"fmt"
	"log"

	billingrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/billingrecovery"
)

type BillingRecoveryLogObserver struct {
	logger *log.Logger
}

func NewBillingRecoveryLogObserver(
	logger *log.Logger,
) (*BillingRecoveryLogObserver, error) {
	if logger == nil {
		return nil, fmt.Errorf("billing recovery logger is nil")
	}
	return &BillingRecoveryLogObserver{logger: logger}, nil
}

func (o *BillingRecoveryLogObserver) ObserveBillingRecoveryCycle(
	cycle billingrecovery.Cycle,
) {
	if cycle.Err != nil {
		o.logger.Printf(
			"billing recovery cycle failed: discovered=%d processed=%d error=%v",
			len(cycle.Result.DiscoveredBatchIDs),
			len(cycle.Result.ProcessedBatchIDs),
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"billing recovery cycle completed: discovered=%d processed=%d",
		len(cycle.Result.DiscoveredBatchIDs),
		len(cycle.Result.ProcessedBatchIDs),
	)
}
