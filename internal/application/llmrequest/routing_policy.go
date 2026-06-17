package llmrequest

import (
	"fmt"
	"time"
)

type RoutingPolicyInput struct {
	UpstreamTimeout       time.Duration
	UpstreamMaxAttempts   int
	UpstreamMaxBackoff    time.Duration
	RateLimitMaxWait      time.Duration
	CooldownRateLimit     time.Duration
	CooldownQuotaExceeded time.Duration
	Cooldown5XX           time.Duration
	CooldownTimeout       time.Duration
	CooldownAuthError     time.Duration
}

type RoutingPolicy struct {
	upstreamTimeout       time.Duration
	upstreamMaxAttempts   int
	upstreamMaxBackoff    time.Duration
	rateLimitMaxWait      time.Duration
	cooldownRateLimit     time.Duration
	cooldownQuotaExceeded time.Duration
	cooldown5XX           time.Duration
	cooldownTimeout       time.Duration
	cooldownAuthError     time.Duration
}

func NewRoutingPolicy(input RoutingPolicyInput) (RoutingPolicy, error) {
	switch {
	case input.UpstreamTimeout <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"upstream timeout must be positive",
		)
	case input.UpstreamMaxAttempts < 1:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"upstream max attempts must be at least one",
		)
	case input.UpstreamMaxBackoff <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"upstream max backoff must be positive",
		)
	case input.RateLimitMaxWait <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"rate-limit max wait must be positive",
		)
	case input.CooldownRateLimit <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"rate-limit cooldown must be positive",
		)
	case input.CooldownQuotaExceeded <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"quota-exceeded cooldown must be positive",
		)
	case input.Cooldown5XX <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"provider-5xx cooldown must be positive",
		)
	case input.CooldownTimeout <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"timeout cooldown must be positive",
		)
	case input.CooldownAuthError <= 0:
		return RoutingPolicy{}, invalidRoutingPolicy(
			"auth-error cooldown must be positive",
		)
	}

	return RoutingPolicy{
		upstreamTimeout:       input.UpstreamTimeout,
		upstreamMaxAttempts:   input.UpstreamMaxAttempts,
		upstreamMaxBackoff:    input.UpstreamMaxBackoff,
		rateLimitMaxWait:      input.RateLimitMaxWait,
		cooldownRateLimit:     input.CooldownRateLimit,
		cooldownQuotaExceeded: input.CooldownQuotaExceeded,
		cooldown5XX:           input.Cooldown5XX,
		cooldownTimeout:       input.CooldownTimeout,
		cooldownAuthError:     input.CooldownAuthError,
	}, nil
}

func (policy RoutingPolicy) UpstreamTimeout() time.Duration {
	return policy.upstreamTimeout
}

func (policy RoutingPolicy) UpstreamMaxAttempts() int {
	return policy.upstreamMaxAttempts
}

func (policy RoutingPolicy) UpstreamMaxBackoff() time.Duration {
	return policy.upstreamMaxBackoff
}

func (policy RoutingPolicy) RateLimitMaxWait() time.Duration {
	return policy.rateLimitMaxWait
}

func (policy RoutingPolicy) CooldownRateLimit() time.Duration {
	return policy.cooldownRateLimit
}

func (policy RoutingPolicy) CooldownQuotaExceeded() time.Duration {
	return policy.cooldownQuotaExceeded
}

func (policy RoutingPolicy) Cooldown5XX() time.Duration {
	return policy.cooldown5XX
}

func (policy RoutingPolicy) CooldownTimeout() time.Duration {
	return policy.cooldownTimeout
}

func (policy RoutingPolicy) CooldownAuthError() time.Duration {
	return policy.cooldownAuthError
}

func invalidRoutingPolicy(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}
