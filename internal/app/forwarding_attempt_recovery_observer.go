package app

import (
	"errors"
	"log"

	forwardingattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/forwardingattemptrecovery"
)

var ErrInvalidForwardingAttemptRecoveryObserver = errors.New(
	"invalid forwarding attempt recovery observer",
)

type ForwardingAttemptRecoveryLogObserver struct {
	logger *log.Logger
}

func NewForwardingAttemptRecoveryLogObserver(
	logger *log.Logger,
) (*ForwardingAttemptRecoveryLogObserver, error) {
	if logger == nil {
		return nil,
			ErrInvalidForwardingAttemptRecoveryObserver
	}
	return &ForwardingAttemptRecoveryLogObserver{
		logger: logger,
	}, nil
}

func (o *ForwardingAttemptRecoveryLogObserver) ObserveForwardingAttemptRecoveryCycle(
	cycle forwardingattemptrecovery.Cycle,
) {
	if o == nil || o.logger == nil {
		return
	}
	if cycle.Err != nil {
		o.logger.Printf(
			"forwarding attempt recovery cycle failed loaded=%d completed=%d error_type=%T",
			cycle.Result.Loaded,
			cycle.Result.Completed,
			cycle.Err,
		)
		return
	}
	o.logger.Printf(
		"forwarding attempt recovery cycle completed loaded=%d completed=%d",
		cycle.Result.Loaded,
		cycle.Result.Completed,
	)
}

var _ forwardingattemptrecovery.Observer = (*ForwardingAttemptRecoveryLogObserver)(nil)
