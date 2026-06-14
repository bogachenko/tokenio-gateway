package ports

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ResellerBalanceReserveResult struct {
	Applied  bool
	Reseller domain.Reseller
}

type ResellerBalanceStore interface {
	// ReserveEstimatedUpstreamCost locks the reseller row, re-evaluates the
	// canonical enabled/balance/minimum-balance rule, and increments
	// reserved_cents only when the complete amount remains available.
	//
	// Applied=false is a deterministic no-op for a disabled reseller or
	// insufficient available balance. Reseller contains the exact persisted
	// snapshot observed under the row lock.
	ReserveEstimatedUpstreamCost(
		context.Context,
		string,
		int64,
		time.Time,
	) (ResellerBalanceReserveResult, error)
}
