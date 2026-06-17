package llmrequest

import (
	"errors"
	"testing"
	"time"
)

func TestRoutingPolicyIsValidatedImmutableValue(t *testing.T) {
	input := validRoutingPolicyInput()

	policy, err := NewRoutingPolicy(input)
	if err != nil {
		t.Fatalf("NewRoutingPolicy: %v", err)
	}

	input.UpstreamTimeout = time.Hour
	input.UpstreamMaxAttempts = 99
	input.CooldownAuthError = time.Second

	if policy.UpstreamTimeout() != 90*time.Second ||
		policy.UpstreamMaxAttempts() != 3 ||
		policy.UpstreamMaxBackoff() != 2*time.Second ||
		policy.RateLimitMaxWait() != 5*time.Second ||
		policy.CooldownRateLimit() != time.Minute ||
		policy.CooldownQuotaExceeded() != 24*time.Hour ||
		policy.Cooldown5XX() != 30*time.Second ||
		policy.CooldownTimeout() != 30*time.Second ||
		policy.CooldownAuthError() != 24*time.Hour {
		t.Fatalf("policy changed after input mutation: %+v", policy)
	}
}

func TestRoutingPolicyRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RoutingPolicyInput)
	}{
		{
			name: "upstream timeout",
			mutate: func(input *RoutingPolicyInput) {
				input.UpstreamTimeout = 0
			},
		},
		{
			name: "max attempts",
			mutate: func(input *RoutingPolicyInput) {
				input.UpstreamMaxAttempts = 0
			},
		},
		{
			name: "max backoff",
			mutate: func(input *RoutingPolicyInput) {
				input.UpstreamMaxBackoff = 0
			},
		},
		{
			name: "rate limit max wait",
			mutate: func(input *RoutingPolicyInput) {
				input.RateLimitMaxWait = 0
			},
		},
		{
			name: "rate limit cooldown",
			mutate: func(input *RoutingPolicyInput) {
				input.CooldownRateLimit = 0
			},
		},
		{
			name: "quota cooldown",
			mutate: func(input *RoutingPolicyInput) {
				input.CooldownQuotaExceeded = 0
			},
		},
		{
			name: "5xx cooldown",
			mutate: func(input *RoutingPolicyInput) {
				input.Cooldown5XX = 0
			},
		},
		{
			name: "timeout cooldown",
			mutate: func(input *RoutingPolicyInput) {
				input.CooldownTimeout = 0
			},
		},
		{
			name: "auth cooldown",
			mutate: func(input *RoutingPolicyInput) {
				input.CooldownAuthError = 0
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRoutingPolicyInput()
			test.mutate(&input)

			_, err := NewRoutingPolicy(input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want invalid input", err)
			}
		})
	}
}

func validRoutingPolicyInput() RoutingPolicyInput {
	return RoutingPolicyInput{
		UpstreamTimeout:       90 * time.Second,
		UpstreamMaxAttempts:   3,
		UpstreamMaxBackoff:    2 * time.Second,
		RateLimitMaxWait:      5 * time.Second,
		CooldownRateLimit:     time.Minute,
		CooldownQuotaExceeded: 24 * time.Hour,
		Cooldown5XX:           30 * time.Second,
		CooldownTimeout:       30 * time.Second,
		CooldownAuthError:     24 * time.Hour,
	}
}
