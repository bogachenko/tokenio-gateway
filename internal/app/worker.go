package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
)

var ErrInvalidWorkerGraph = errors.New(
	"invalid worker graph",
)

type WorkerRunner interface {
	Run(context.Context) error
}

type WorkerGraph struct {
	ProvisioningExpirationEnabled bool
	ProvisioningExpiration        WorkerRunner
}

func NewWorkerGraph(
	cfg config.Config,
	applications ApplicationGraph,
	observer provisioningexpiration.Observer,
) (WorkerGraph, error) {
	if err := applications.Validate(); err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"validate application graph: %w",
			err,
		)
	}

	if !applications.ProvisioningEnabled {
		graph := WorkerGraph{}
		if err := graph.Validate(); err != nil {
			return WorkerGraph{}, fmt.Errorf(
				"validate disabled worker graph: %w",
				err,
			)
		}
		return graph, nil
	}

	worker, err := provisioningexpiration.New(
		applications.Provisioning,
		observer,
		cfg.APIKeyProvisioningExpirationInterval,
		cfg.APIKeyProvisioningExpirationBatchSize,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct provisioning expiration worker: %w",
			err,
		)
	}

	graph := WorkerGraph{
		ProvisioningExpirationEnabled: true,
		ProvisioningExpiration:        worker,
	}
	if err := graph.Validate(); err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"validate worker graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g WorkerGraph) Validate() error {
	switch {
	case g.ProvisioningExpirationEnabled &&
		g.ProvisioningExpiration == nil:
		return fmt.Errorf(
			"enabled provisioning expiration worker is nil",
		)
	case !g.ProvisioningExpirationEnabled &&
		g.ProvisioningExpiration != nil:
		return fmt.Errorf(
			"disabled provisioning expiration worker is non-nil",
		)
	default:
		return nil
	}
}

func (g WorkerGraph) Run(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidWorkerGraph
	}
	if err := g.Validate(); err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrInvalidWorkerGraph,
			err,
		)
	}

	if !g.ProvisioningExpirationEnabled {
		<-ctx.Done()
		return nil
	}
	return g.ProvisioningExpiration.Run(ctx)
}
