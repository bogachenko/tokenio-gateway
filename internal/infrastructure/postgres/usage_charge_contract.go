package postgres

import "github.com/bogachenko/tokenio-gateway/internal/ports"

var _ ports.UsageLedger = (*UsageLedger)(nil)
