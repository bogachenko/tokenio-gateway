package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

func NewServer(cfg config.Config) *http.Server {
	return NewServerWithHandler(
		cfg,
		httptransport.NewRouter(),
	)
}

func NewServerWithHandler(
	cfg config.Config,
	handler http.Handler,
) *http.Server {
	if handler == nil {
		panic("app: nil HTTP handler")
	}
	return &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	return RunWithConfig(ctx, cfg)
}

func RunWithConfig(
	ctx context.Context,
	cfg config.Config,
) error {
	runtime, err := NewRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()

	server := NewServerWithHandler(cfg, runtime.Handler)
	log.Printf(
		"tokenio-gateway listening on %s",
		cfg.GatewayAddr,
	)
	return serveHTTP(
		ctx,
		server,
		cfg.HTTPShutdownTimeout,
	)
}

func serveHTTP(
	ctx context.Context,
	server *http.Server,
	shutdownTimeout time.Duration,
) error {
	if ctx == nil ||
		server == nil ||
		server.Handler == nil ||
		shutdownTimeout <= 0 {
		return fmt.Errorf("invalid HTTP runtime configuration")
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		shutdownErr := server.Shutdown(shutdownContext)
		cancel()

		if shutdownErr != nil {
			_ = server.Close()
		}
		serveErr := <-serveErrors

		if shutdownErr != nil {
			return fmt.Errorf(
				"graceful HTTP shutdown: %w",
				shutdownErr,
			)
		}
		if serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}
