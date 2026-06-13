package app

import (
	"context"
	"errors"
	"fmt"
	"log"

	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
)

var ErrInvalidProvisioningExpirationObserver = errors.New(
	"invalid provisioning expiration observer",
)

type ProvisioningExpirationLogObserver struct {
	logger *log.Logger
}

func NewProvisioningExpirationLogObserver(
	logger *log.Logger,
) (*ProvisioningExpirationLogObserver, error) {
	if logger == nil {
		return nil,
			ErrInvalidProvisioningExpirationObserver
	}
	return &ProvisioningExpirationLogObserver{
		logger: logger,
	}, nil
}

func (o *ProvisioningExpirationLogObserver) ObserveProvisioningExpirationCycle(
	cycle provisioningexpiration.Cycle,
) {
	if o == nil || o.logger == nil {
		return
	}

	o.logger.Printf(
		"provisioning expiration cycle error_code=%s as_of=%s selected=%d expired=%d already_terminal=%d failed=%d",
		provisioningExpirationErrorCode(cycle.Err),
		cycle.Result.AsOf.UTC().Format(
			"2006-01-02T15:04:05.000000000Z",
		),
		cycle.Result.Selected,
		cycle.Result.Expired,
		cycle.Result.AlreadyTerminal,
		cycle.Result.Failed,
	)
}

func provisioningExpirationErrorCode(
	err error,
) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(
		err,
		context.DeadlineExceeded,
	):
		return "context_deadline_exceeded"
	case errors.Is(
		err,
		provisioningapp.ErrExpirationPartialFailure,
	):
		return "partial_failure"
	case errors.Is(
		err,
		provisioningapp.ErrStoreUnavailable,
	):
		return "store_unavailable"
	case errors.Is(
		err,
		provisioningapp.ErrInternal,
	):
		return "internal"
	default:
		return "unexpected"
	}
}

var _ provisioningexpiration.Observer = (*ProvisioningExpirationLogObserver)(nil)
var _ fmt.Stringer = (*ProvisioningExpirationLogObserver)(nil)

func (*ProvisioningExpirationLogObserver) String() string {
	return "ProvisioningExpirationLogObserver"
}
