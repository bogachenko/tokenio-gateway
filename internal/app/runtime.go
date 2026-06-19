package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
)

type Runtime struct {
	Config       config.Config
	Primitives   RuntimePrimitives
	Security     SecurityGraph
	Provisioning ProvisioningInfrastructureGraph
	Billing      BillingInfrastructureGraph
	Forwarding   ForwardingInfrastructureGraph
	Telegram     TelegramInfrastructureGraph
	Repositories RepositoryGraph
	Applications ApplicationGraph
	Workers      WorkerGraph
	Transports   TransportGraph
	Logging      LoggingGraph
	Handler      http.Handler

	database  *postgres.DB
	closeOnce sync.Once
}

func NewRuntime(
	ctx context.Context,
	cfg config.Config,
	provisioningExpirationObserver provisioningexpiration.Observer,
) (*Runtime, error) {
	primitives, err := NewRuntimePrimitives()
	if err != nil {
		return nil, err
	}

	loggingGraph, err := NewLoggingGraph(cfg, os.Stderr)
	if err != nil {
		return nil, err
	}

	security, err := NewSecurityGraph(cfg)
	if err != nil {
		return nil, err
	}

	provisioningInfrastructure, err :=
		NewProvisioningInfrastructureGraph(
			cfg,
			security,
		)
	if err != nil {
		return nil, err
	}

	billingInfrastructure, err :=
		NewBillingInfrastructureGraph(
			cfg,
			primitives.Clock,
		)
	if err != nil {
		return nil, err
	}

	forwardingInfrastructure, err :=
		NewForwardingInfrastructureGraph()
	if err != nil {
		return nil, err
	}

	telegramInfrastructure, err :=
		NewTelegramInfrastructureGraph(cfg)
	if err != nil {
		return nil, err
	}

	database, err := postgres.Open(
		ctx,
		cfg.DatabaseDSN,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open PostgreSQL: %w",
			err,
		)
	}

	closed := false
	closeDatabase := func() {
		if closed {
			return
		}
		closed = true
		database.Close()
	}

	if err := database.ValidateSchema(ctx); err != nil {
		closeDatabase()
		return nil, fmt.Errorf(
			"validate PostgreSQL schema: %w",
			err,
		)
	}

	repositories, err := NewRepositoryGraph(
		database,
		primitives.Clock,
	)
	if err != nil {
		closeDatabase()
		return nil, err
	}
	if err := repositories.Validate(); err != nil {
		closeDatabase()
		return nil, fmt.Errorf(
			"validate repository graph: %w",
			err,
		)
	}

	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		telegramInfrastructure,
		loggingGraph,
		repositories,
	)
	if err != nil {
		closeDatabase()
		return nil, err
	}

	workers, err := NewWorkerGraph(
		cfg,
		applications,
		loggingGraph,
		provisioningExpirationObserver,
	)
	if err != nil {
		closeDatabase()
		return nil, err
	}

	transports, err := NewTransportGraph(
		cfg,
		primitives,
		security,
		applications,
	)
	if err != nil {
		closeDatabase()
		return nil, err
	}

	runtime := &Runtime{
		Config:       cfg,
		Primitives:   primitives,
		Security:     security,
		Provisioning: provisioningInfrastructure,
		Billing:      billingInfrastructure,
		Forwarding:   forwardingInfrastructure,
		Telegram:     telegramInfrastructure,
		Repositories: repositories,
		Applications: applications,
		Workers:      workers,
		Transports:   transports,
		Logging:      loggingGraph,
		Handler:      transports.Root,
		database:     database,
	}
	return runtime, nil
}

func (r *Runtime) Ping(ctx context.Context) error {
	if r == nil || r.database == nil {
		return postgres.ErrInvalidDatabaseConfig
	}
	return r.database.Ping(ctx)
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		if r.database != nil {
			r.database.Close()
		}
	})
}
