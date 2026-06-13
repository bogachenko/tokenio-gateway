package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

type billingGraphClock struct {
	now time.Time
}

func (c billingGraphClock) Now() time.Time {
	return c.now
}

type billingGraphRoundTripper struct {
	requests []*http.Request
}

func (r *billingGraphRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	r.requests = append(r.requests, request)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"currency":"RUB","balance_cents":1000}`,
		)),
	}, nil
}

func validBillingGraphConfig() config.Config {
	return config.Config{
		BillingBaseURL:       "https://billing.example",
		BillingServiceToken:  "billing-service-token",
		BillingJWTSigningKey: "billing-jwt-signing-key",
		BillingJWTTTL:        15 * time.Minute,
		BillingTimeout:       30 * time.Second,
	}
}

func TestNewBillingInfrastructureGraphConstructsRequiredCapabilities(
	t *testing.T,
) {
	roundTripper := &billingGraphRoundTripper{}
	clock := billingGraphClock{
		now: time.Date(
			2026,
			time.June,
			13,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}

	graph, err := newBillingInfrastructureGraph(
		validBillingGraphConfig(),
		clock,
		roundTripper,
	)
	if err != nil {
		t.Fatalf("newBillingInfrastructureGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("billing infrastructure graph: %v", err)
	}

	token, err := graph.Identity.TokenForSubject(
		context.Background(),
		"billing-user",
	)
	if err != nil {
		t.Fatalf("TokenForSubject: %v", err)
	}
	if token == "" {
		t.Fatal("billing identity token is empty")
	}

	balance, err := graph.Balance.GetBalance(
		context.Background(),
		token,
	)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if balance.Currency != "RUB" || balance.BalanceCents != 1000 {
		t.Fatalf("balance = %+v", balance)
	}
	if len(roundTripper.requests) != 1 {
		t.Fatalf(
			"billing round trips = %d, want 1",
			len(roundTripper.requests),
		)
	}
}

func TestNewBillingInfrastructureGraphFailsFast(
	t *testing.T,
) {
	valid := validBillingGraphConfig()
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name: "base URL",
			mutate: func(cfg *config.Config) {
				cfg.BillingBaseURL = ""
			},
		},
		{
			name: "service token",
			mutate: func(cfg *config.Config) {
				cfg.BillingServiceToken = ""
			},
		},
		{
			name: "JWT signing key",
			mutate: func(cfg *config.Config) {
				cfg.BillingJWTSigningKey = ""
			},
		},
		{
			name: "JWT TTL",
			mutate: func(cfg *config.Config) {
				cfg.BillingJWTTTL = 0
			},
		},
		{
			name: "HTTP timeout",
			mutate: func(cfg *config.Config) {
				cfg.BillingTimeout = 0
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.mutate(&cfg)

			graph, err := newBillingInfrastructureGraph(
				cfg,
				billingGraphClock{now: time.Now().UTC()},
				&billingGraphRoundTripper{},
			)
			if err == nil {
				t.Fatal("expected startup construction error")
			}
			if err := graph.Validate(); err == nil {
				t.Fatal("invalid graph unexpectedly validated")
			}
		})
	}
}

func TestBillingInfrastructureGraphRejectsMissingDependency(
	t *testing.T,
) {
	var graph BillingInfrastructureGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}
