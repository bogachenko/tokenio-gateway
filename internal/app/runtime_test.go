package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
)

func TestNewRepositoryGraphRejectsNilDatabase(t *testing.T) {
	_, err := NewRepositoryGraph(nil)
	if !errors.Is(err, postgres.ErrInvalidDatabaseConfig) {
		t.Fatalf(
			"error = %v, want invalid database config",
			err,
		)
	}
}

func TestRepositoryGraphValidateRejectsMissingCapability(
	t *testing.T,
) {
	var graph RepositoryGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}

type runtimeTestHandler struct{}

func (*runtimeTestHandler) ServeHTTP(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	writer.WriteHeader(http.StatusNoContent)
}

func TestNewServerWithHandlerUsesExactHandler(t *testing.T) {
	handler := &runtimeTestHandler{}
	cfg := config.Config{
		GatewayAddr:           "127.0.0.1:0",
		HTTPReadHeaderTimeout: time.Second,
		HTTPReadTimeout:       2 * time.Second,
		HTTPWriteTimeout:      3 * time.Second,
		HTTPIdleTimeout:       4 * time.Second,
	}

	server := NewServerWithHandler(cfg, handler)
	if server.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if server.Handler != handler {
		t.Fatal("server did not preserve exact composed handler")
	}
}

func TestNewRuntimeIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	cfg := config.Config{
		DatabaseDSN:           dsn,
		GatewayAddr:           "127.0.0.1:0",
		HTTPReadHeaderTimeout: time.Second,
		HTTPReadTimeout:       2 * time.Second,
		HTTPWriteTimeout:      3 * time.Second,
		HTTPIdleTimeout:       4 * time.Second,
		HTTPShutdownTimeout:   5 * time.Second,
	}

	runtime, err := NewRuntime(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Close)

	if err := runtime.Repositories.Validate(); err != nil {
		t.Fatalf("repository graph: %v", err)
	}
	if runtime.Handler == nil {
		t.Fatal("runtime handler is nil")
	}
	if err := runtime.Ping(context.Background()); err != nil {
		t.Fatalf("runtime ping: %v", err)
	}

	runtime.Close()
	runtime.Close()
}
