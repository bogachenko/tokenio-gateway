package app

import (
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func assembleRoutingPolicy(
	cfg config.Config,
) (llmrequest.RoutingPolicy, error) {
	return llmrequest.NewRoutingPolicy(
		llmrequest.RoutingPolicyInput{
			UpstreamTimeout:       cfg.UpstreamTimeout,
			UpstreamMaxAttempts:   cfg.UpstreamMaxAttempts,
			UpstreamMaxBackoff:    cfg.UpstreamMaxBackoff,
			RateLimitMaxWait:      cfg.RateLimitMaxWait,
			CooldownRateLimit:     cfg.CooldownRateLimit,
			CooldownQuotaExceeded: cfg.CooldownQuotaExceeded,
			Cooldown5XX:           cfg.Cooldown5XX,
			CooldownTimeout:       cfg.CooldownTimeout,
			CooldownAuthError:     cfg.CooldownAuthError,
		},
	)
}
