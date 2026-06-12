package ledger

import (
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Service struct {
	ledger ports.UsageLedger
	clock  ports.Clock
}

func NewService(usageLedger ports.UsageLedger, clock ports.Clock) (*Service, error) {
	if usageLedger == nil {
		return nil, fmt.Errorf("%w: nil usage ledger", ErrInvalidLedgerInput)
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: nil clock", ErrInvalidLedgerInput)
	}
	return &Service{ledger: usageLedger, clock: clock}, nil
}

func (s *Service) operationTime() (time.Time, error) {
	now := s.clock.Now()
	if now.IsZero() {
		return time.Time{}, fmt.Errorf("%w: zero clock", ErrInvalidLedgerInput)
	}
	return now.UTC(), nil
}

func timePtr(value time.Time) *time.Time {
	copyValue := value
	return &copyValue
}
