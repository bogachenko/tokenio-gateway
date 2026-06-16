package app

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	billingrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/billingrecovery"
	forwardingattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/forwardingattemptrecovery"
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

	ForwardingAttemptRecoveryEnabled bool
	ForwardingAttemptRecovery        WorkerRunner

	BillingRecoveryEnabled bool
	BillingRecovery        WorkerRunner
}

func NewWorkerGraph(
	cfg config.Config,
	applications ApplicationGraph,
	provisioningObserver provisioningexpiration.Observer,
) (WorkerGraph, error) {
	recoveryObserver, err :=
		NewForwardingAttemptRecoveryLogObserver(
			log.Default(),
		)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct forwarding attempt recovery observer: %w",
			err,
		)
	}
	billingObserver, err := NewBillingRecoveryLogObserver(
		log.Default(),
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct billing recovery observer: %w",
			err,
		)
	}
	return newWorkerGraphWithObservers(
		cfg,
		applications,
		provisioningObserver,
		recoveryObserver,
		billingObserver,
	)
}

func newWorkerGraphWithObservers(
	cfg config.Config,
	applications ApplicationGraph,
	provisioningObserver provisioningexpiration.Observer,
	recoveryObserver forwardingattemptrecovery.Observer,
	billingObserver billingrecovery.Observer,
) (WorkerGraph, error) {
	if err := applications.Validate(); err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"validate application graph: %w",
			err,
		)
	}

	recoveryWorker, err := forwardingattemptrecovery.New(
		applications.ForwardingAttemptRecovery,
		recoveryObserver,
		cfg.ForwardingAttemptRecoveryInterval,
		cfg.ForwardingAttemptRecoveryBatchSize,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct forwarding attempt recovery worker: %w",
			err,
		)
	}

	billingWorker, err := billingrecovery.New(
		applications.BillingRecovery,
		billingObserver,
		cfg.BillingRecoveryInterval,
		cfg.BillingRecoveryBatchSize,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct billing recovery worker: %w",
			err,
		)
	}

	graph := WorkerGraph{
		ForwardingAttemptRecoveryEnabled: true,
		ForwardingAttemptRecovery:        recoveryWorker,
		BillingRecoveryEnabled:           true,
		BillingRecovery:                  billingWorker,
	}
	if applications.ProvisioningEnabled {
		provisioningWorker, err := provisioningexpiration.New(
			applications.Provisioning,
			provisioningObserver,
			cfg.APIKeyProvisioningExpirationInterval,
			cfg.APIKeyProvisioningExpirationBatchSize,
		)
		if err != nil {
			return WorkerGraph{}, fmt.Errorf(
				"construct provisioning expiration worker: %w",
				err,
			)
		}
		graph.ProvisioningExpirationEnabled = true
		graph.ProvisioningExpiration = provisioningWorker
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
	case g.ForwardingAttemptRecoveryEnabled &&
		g.ForwardingAttemptRecovery == nil:
		return fmt.Errorf(
			"enabled forwarding attempt recovery worker is nil",
		)
	case !g.ForwardingAttemptRecoveryEnabled &&
		g.ForwardingAttemptRecovery != nil:
		return fmt.Errorf(
			"disabled forwarding attempt recovery worker is non-nil",
		)
	case g.BillingRecoveryEnabled &&
		g.BillingRecovery == nil:
		return fmt.Errorf(
			"enabled billing recovery worker is nil",
		)
	case !g.BillingRecoveryEnabled &&
		g.BillingRecovery != nil:
		return fmt.Errorf(
			"disabled billing recovery worker is non-nil",
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

	runners := make([]WorkerRunner, 0, 3)
	if g.ProvisioningExpirationEnabled {
		runners = append(
			runners,
			g.ProvisioningExpiration,
		)
	}
	if g.ForwardingAttemptRecoveryEnabled {
		runners = append(
			runners,
			g.ForwardingAttemptRecovery,
		)
	}
	if g.BillingRecoveryEnabled {
		runners = append(
			runners,
			g.BillingRecovery,
		)
	}
	if len(runners) == 0 {
		<-ctx.Done()
		return nil
	}

	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, len(runners))
	for _, runner := range runners {
		current := runner
		go func() {
			results <- current.Run(runContext)
		}()
	}

	var result error
	for range runners {
		currentErr := <-results
		if result == nil {
			result = currentErr
			cancel()
		} else {
			result = errors.Join(result, currentErr)
		}
	}
	return result
}
