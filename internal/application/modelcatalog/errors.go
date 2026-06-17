package modelcatalog

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidInput       = errors.New("invalid model catalog input")
	ErrCatalogUnavailable = &ports.ApplicationError{
		Code:         domain.ErrorCodeStoreUnavailable,
		SafeMessage:  "Store is unavailable",
		Category:     ports.FailureCategoryUnavailable,
		Retryability: ports.RetryabilityRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("model catalog unavailable"),
	}
)
