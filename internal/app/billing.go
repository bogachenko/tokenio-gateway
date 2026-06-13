package app

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	billinghttp "github.com/bogachenko/tokenio-gateway/internal/infrastructure/billing/httpclient"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/billing/jwtidentity"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type BillingInfrastructureGraph struct {
	Identity ports.BillingIdentityService
	Balance  ports.BillingBalanceClient
	Charge   ports.BillingChargeClient
}

func NewBillingInfrastructureGraph(
	cfg config.Config,
	clock ports.Clock,
) (BillingInfrastructureGraph, error) {
	return newBillingInfrastructureGraph(
		cfg,
		clock,
		http.DefaultTransport,
	)
}

func newBillingInfrastructureGraph(
	cfg config.Config,
	clock ports.Clock,
	roundTripper http.RoundTripper,
) (BillingInfrastructureGraph, error) {
	identity, err := jwtidentity.New(jwtidentity.Config{
		SigningKey: []byte(cfg.BillingJWTSigningKey),
		TTL:        cfg.BillingJWTTTL,
		Clock:      clock,
	})
	if err != nil {
		return BillingInfrastructureGraph{}, fmt.Errorf(
			"construct billing identity service: %w",
			err,
		)
	}

	client, err := billinghttp.New(billinghttp.Config{
		BaseURL:              cfg.BillingBaseURL,
		ServiceToken:         cfg.BillingServiceToken,
		RoundTripper:         roundTripper,
		Timeout:              cfg.BillingTimeout,
		MaxResponseBodyBytes: billinghttp.DefaultMaxResponseBodyBytes,
	})
	if err != nil {
		return BillingInfrastructureGraph{}, fmt.Errorf(
			"construct billing HTTP client: %w",
			err,
		)
	}

	graph := BillingInfrastructureGraph{
		Identity: identity,
		Balance:  client,
		Charge:   client,
	}
	if err := graph.Validate(); err != nil {
		return BillingInfrastructureGraph{}, fmt.Errorf(
			"validate billing infrastructure graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g BillingInfrastructureGraph) Validate() error {
	switch {
	case g.Identity == nil:
		return fmt.Errorf("billing identity service is nil")
	case g.Balance == nil:
		return fmt.Errorf("billing balance client is nil")
	case g.Charge == nil:
		return fmt.Errorf("billing charge client is nil")
	default:
		return nil
	}
}
