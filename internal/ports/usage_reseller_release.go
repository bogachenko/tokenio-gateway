package ports

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type UsageResellerReleaseResult struct {
	Applied bool
	Usage   domain.UsageRecord
}

type UsageResellerReleaseStore interface {
	// ReleaseReservedUsageAndResellerReserve atomically transitions one persisted
	// usage record from reserved to released and releases its persisted
	// estimated_upstream_cost_cents from the selected reseller.
	//
	// The usage row is the durable release identity. Applied=false returns the
	// exact current usage record and performs no reseller balance mutation.
	ReleaseReservedUsageAndResellerReserve(
		context.Context,
		string,
		string,
		time.Time,
	) (UsageResellerReleaseResult, error)
}
