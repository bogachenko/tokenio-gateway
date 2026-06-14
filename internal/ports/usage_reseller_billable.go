package ports

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type UsageResellerBillableResult struct {
	Applied  bool
	Usage    domain.UsageRecord
	Reseller domain.Reseller
}

type UsageResellerBillableStore interface {
	// CommitReservedUsageAndReconcileReseller atomically transitions one usage
	// record from reserved to billable, releases its persisted estimated reseller
	// reserve, and subtracts the persisted actual upstream cost from balance.
	//
	// The usage row is the durable reconciliation identity. Applied=false returns
	// the exact current usage record and performs no reseller balance mutation.
	CommitReservedUsageAndReconcileReseller(
		context.Context,
		string,
		domain.UsageRecord,
	) (UsageResellerBillableResult, error)
}
