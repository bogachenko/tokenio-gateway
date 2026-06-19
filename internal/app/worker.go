package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	billingrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/billingrecovery"
	forwardingattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/forwardingattemptrecovery"
	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
	telegrambalancescan "github.com/bogachenko/tokenio-gateway/internal/worker/telegrambalancescan"
	telegramdelivery "github.com/bogachenko/tokenio-gateway/internal/worker/telegramdelivery"
	telegramfailedretry "github.com/bogachenko/tokenio-gateway/internal/worker/telegramfailedretry"
	telegramstaleattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/telegramstaleattemptrecovery"
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

	TelegramDeliveryEnabled bool
	TelegramDelivery        WorkerRunner

	TelegramFailedRetryEnabled bool
	TelegramFailedRetry        WorkerRunner

	TelegramBalanceScanEnabled bool
	TelegramBalanceScan        WorkerRunner

	TelegramStaleAttemptRecoveryEnabled bool
	TelegramStaleAttemptRecovery        WorkerRunner
}

func NewWorkerGraph(
	cfg config.Config,
	applications ApplicationGraph,
	loggingGraph LoggingGraph,
	provisioningObserver provisioningexpiration.Observer,
) (WorkerGraph, error) {
	if err := loggingGraph.Validate(); err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"validate logging graph: %w",
			err,
		)
	}

	recoveryObserver, err :=
		NewForwardingAttemptRecoveryLogObserver(
			loggingGraph.StdLogger,
		)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct forwarding attempt recovery observer: %w",
			err,
		)
	}
	billingObserver, err := NewBillingRecoveryLogObserver(
		loggingGraph.StdLogger,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct billing recovery observer: %w",
			err,
		)
	}
	telegramDeliveryObserver, err := NewTelegramDeliveryLogObserver(
		loggingGraph.StdLogger,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct Telegram delivery observer: %w",
			err,
		)
	}
	telegramFailedRetryObserver, err := NewTelegramFailedRetryLogObserver(
		loggingGraph.StdLogger,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct Telegram failed-retry observer: %w",
			err,
		)
	}
	telegramBalanceScanObserver, err := NewTelegramBalanceScanLogObserver(
		loggingGraph.StdLogger,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct Telegram balance-scan observer: %w",
			err,
		)
	}
	return newWorkerGraphWithObservers(
		cfg,
		applications,
		loggingGraph,
		provisioningObserver,
		recoveryObserver,
		billingObserver,
		telegramDeliveryObserver,
		telegramFailedRetryObserver,
		telegramBalanceScanObserver,
	)
}

func newWorkerGraphWithObservers(
	cfg config.Config,
	applications ApplicationGraph,
	loggingGraph LoggingGraph,
	provisioningObserver provisioningexpiration.Observer,
	recoveryObserver forwardingattemptrecovery.Observer,
	billingObserver billingrecovery.Observer,
	telegramDeliveryObserver telegramdelivery.Observer,
	telegramFailedRetryObserver telegramfailedretry.Observer,
	telegramBalanceScanObserver telegrambalancescan.Observer,
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

	var telegramDeliveryWorker WorkerRunner
	if applications.TelegramDeliveryEnabled {
		telegramDeliveryWorker, err = telegramdelivery.New(
			applications.TelegramAlertStore,
			applications.TelegramDelivery,
			telegramDeliveryObserver,
			cfg.TelegramDeliveryInterval,
			cfg.TelegramDeliveryBatchSize,
		)
		if err != nil {
			return WorkerGraph{}, fmt.Errorf(
				"construct Telegram delivery worker: %w",
				err,
			)
		}
	}

	var telegramFailedRetryWorker WorkerRunner
	if applications.TelegramDeliveryEnabled {
		telegramFailedRetryWorker, err = telegramfailedretry.New(
			applications.TelegramRecovery,
			telegramFailedRetryObserver,
			cfg.TelegramFailedRetryInterval,
			cfg.TelegramFailedRetryBatchSize,
		)
		if err != nil {
			return WorkerGraph{}, fmt.Errorf(
				"construct Telegram failed-retry worker: %w",
				err,
			)
		}
	}

	var telegramBalanceScanWorker WorkerRunner
	if applications.TelegramDeliveryEnabled {
		telegramBalanceScanWorker, err = telegrambalancescan.New(
			applications.TelegramBalanceScan,
			telegramBalanceScanObserver,
			cfg.TelegramBalanceScanInterval,
			cfg.TelegramBalanceScanBatchSize,
		)
		if err != nil {
			return WorkerGraph{}, fmt.Errorf(
				"construct Telegram balance-scan worker: %w",
				err,
			)
		}
	}

	telegramRecoveryObserver, err :=
		NewTelegramStaleAttemptRecoveryLogObserver(loggingGraph.StdLogger)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct Telegram stale-attempt recovery observer: %w",
			err,
		)
	}
	telegramRecoveryWorker, err := telegramstaleattemptrecovery.New(
		applications.TelegramStaleAttemptRecovery,
		telegramRecoveryObserver,
		cfg.TelegramStaleAttemptRecoveryInterval,
		cfg.TelegramStaleAttemptRecoveryBatchSize,
	)
	if err != nil {
		return WorkerGraph{}, fmt.Errorf(
			"construct Telegram stale-attempt recovery worker: %w",
			err,
		)
	}

	graph := WorkerGraph{
		ForwardingAttemptRecoveryEnabled:    true,
		ForwardingAttemptRecovery:           recoveryWorker,
		BillingRecoveryEnabled:              true,
		BillingRecovery:                     billingWorker,
		TelegramStaleAttemptRecoveryEnabled: true,
		TelegramStaleAttemptRecovery:        telegramRecoveryWorker,
	}
	if applications.TelegramDeliveryEnabled {
		graph.TelegramDeliveryEnabled = true
		graph.TelegramDelivery = telegramDeliveryWorker
		graph.TelegramFailedRetryEnabled = true
		graph.TelegramFailedRetry = telegramFailedRetryWorker
		graph.TelegramBalanceScanEnabled = true
		graph.TelegramBalanceScan = telegramBalanceScanWorker
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
	case g.TelegramDeliveryEnabled &&
		g.TelegramDelivery == nil:
		return fmt.Errorf(
			"enabled Telegram delivery worker is nil",
		)
	case !g.TelegramDeliveryEnabled &&
		g.TelegramDelivery != nil:
		return fmt.Errorf(
			"disabled Telegram delivery worker is non-nil",
		)
	case g.TelegramFailedRetryEnabled &&
		g.TelegramFailedRetry == nil:
		return fmt.Errorf(
			"enabled Telegram failed-retry worker is nil",
		)
	case !g.TelegramFailedRetryEnabled &&
		g.TelegramFailedRetry != nil:
		return fmt.Errorf(
			"disabled Telegram failed-retry worker is non-nil",
		)
	case g.TelegramBalanceScanEnabled &&
		g.TelegramBalanceScan == nil:
		return fmt.Errorf(
			"enabled Telegram balance-scan worker is nil",
		)
	case !g.TelegramBalanceScanEnabled &&
		g.TelegramBalanceScan != nil:
		return fmt.Errorf(
			"disabled Telegram balance-scan worker is non-nil",
		)
	case g.TelegramStaleAttemptRecoveryEnabled &&
		g.TelegramStaleAttemptRecovery == nil:
		return fmt.Errorf(
			"enabled Telegram stale-attempt recovery worker is nil",
		)
	case !g.TelegramStaleAttemptRecoveryEnabled &&
		g.TelegramStaleAttemptRecovery != nil:
		return fmt.Errorf(
			"disabled Telegram stale-attempt recovery worker is non-nil",
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

	runners := make([]WorkerRunner, 0, 7)
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
	if g.TelegramDeliveryEnabled {
		runners = append(
			runners,
			g.TelegramDelivery,
		)
	}
	if g.TelegramFailedRetryEnabled {
		runners = append(
			runners,
			g.TelegramFailedRetry,
		)
	}
	if g.TelegramBalanceScanEnabled {
		runners = append(
			runners,
			g.TelegramBalanceScan,
		)
	}
	if g.TelegramStaleAttemptRecoveryEnabled {
		runners = append(
			runners,
			g.TelegramStaleAttemptRecovery,
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
