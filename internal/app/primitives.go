package app

import (
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/runtimeprimitives"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type RuntimePrimitives struct {
	Clock      ports.Clock
	RequestIDs ports.RequestIDGenerator
}

func NewRuntimePrimitives() (RuntimePrimitives, error) {
	primitives := RuntimePrimitives{
		Clock:      runtimeprimitives.NewUTCClock(),
		RequestIDs: runtimeprimitives.NewSecureRequestIDGenerator(),
	}
	if err := primitives.Validate(); err != nil {
		return RuntimePrimitives{}, fmt.Errorf(
			"validate runtime primitives: %w",
			err,
		)
	}
	return primitives, nil
}

func (p RuntimePrimitives) Validate() error {
	if p.Clock == nil {
		return fmt.Errorf("clock is nil")
	}
	if p.RequestIDs == nil {
		return fmt.Errorf("request ID generator is nil")
	}

	now := p.Clock.Now()
	if now.IsZero() {
		return fmt.Errorf("clock returned zero time")
	}
	if now.Location() != time.UTC {
		return fmt.Errorf("clock must return UTC time")
	}
	return nil
}
