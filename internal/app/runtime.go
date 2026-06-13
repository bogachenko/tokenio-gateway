package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

type Runtime struct {
	Config       config.Config
	Primitives   RuntimePrimitives
	Security     SecurityGraph
	Repositories RepositoryGraph
	Handler      http.Handler

	database  *postgres.DB
	closeOnce sync.Once
}

func NewRuntime(
	ctx context.Context,
	cfg config.Config,
) (*Runtime, error) {
	primitives, err := NewRuntimePrimitives()
	if err != nil {
		return nil, err
	}

	security, err := NewSecurityGraph(cfg)
	if err != nil {
		return nil, err
	}

	database, err := postgres.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}

	closed := false
	closeDatabase := func() {
		if closed {
			return
		}
		closed = true
		database.Close()
	}

	if err := database.ApplyMigrations(ctx); err != nil {
		closeDatabase()
		return nil, fmt.Errorf(
			"apply PostgreSQL migrations: %w",
			err,
		)
	}
	if err := database.ValidateSchema(ctx); err != nil {
		closeDatabase()
		return nil, fmt.Errorf(
			"validate PostgreSQL schema: %w",
			err,
		)
	}

	repositories, err := NewRepositoryGraph(database)
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

	runtime := &Runtime{
		Config:       cfg,
		Primitives:   primitives,
		Security:     security,
		Repositories: repositories,
		Handler:      httptransport.NewRouter(),
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
