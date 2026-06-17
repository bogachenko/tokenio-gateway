package app

import (
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func TestAssembleRoutingPolicyUsesEveryRuntimeConfigField(
	t *testing.T,
) {
	cfg := config.Config{
		UpstreamTimeout:       11 * time.Second,
		UpstreamMaxAttempts:   4,
		UpstreamMaxBackoff:    3 * time.Second,
		RateLimitMaxWait:      7 * time.Second,
		CooldownRateLimit:     13 * time.Second,
		CooldownQuotaExceeded: 17 * time.Second,
		Cooldown5XX:           19 * time.Second,
		CooldownTimeout:       23 * time.Second,
		CooldownAuthError:     29 * time.Second,
	}

	policy, err := assembleRoutingPolicy(cfg)
	if err != nil {
		t.Fatalf("assembleRoutingPolicy: %v", err)
	}

	if policy.UpstreamTimeout() != cfg.UpstreamTimeout ||
		policy.UpstreamMaxAttempts() != cfg.UpstreamMaxAttempts ||
		policy.UpstreamMaxBackoff() != cfg.UpstreamMaxBackoff ||
		policy.RateLimitMaxWait() != cfg.RateLimitMaxWait ||
		policy.CooldownRateLimit() != cfg.CooldownRateLimit ||
		policy.CooldownQuotaExceeded() != cfg.CooldownQuotaExceeded ||
		policy.Cooldown5XX() != cfg.Cooldown5XX ||
		policy.CooldownTimeout() != cfg.CooldownTimeout ||
		policy.CooldownAuthError() != cfg.CooldownAuthError {
		t.Fatalf("policy does not match config: %+v", policy)
	}
}
