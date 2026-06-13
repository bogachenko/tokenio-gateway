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
)

var ErrRuntimeComponentStopped = errors.New(
	"runtime component stopped unexpectedly",
)

type httpRuntime interface {
	ListenAndServe() error
	Shutdown(context.Context) error
	Close() error
}

type workerRuntime interface {
	Run(context.Context) error
}

func NewServer(
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
	observer, err :=
		NewProvisioningExpirationLogObserver(
			log.Default(),
		)
	if err != nil {
		return err
	}

	runtime, err := NewRuntime(
		ctx,
		cfg,
		observer,
	)
	if err != nil {
		return err
	}
	defer runtime.Close()

	server := NewServer(cfg, runtime.Handler)
	log.Printf(
		"tokenio-gateway listening on %s",
		cfg.GatewayAddr,
	)
	return serveRuntime(
		ctx,
		server,
		runtime.Workers,
		cfg.HTTPShutdownTimeout,
	)
}

func serveRuntime(
	ctx context.Context,
	server httpRuntime,
	workers workerRuntime,
	shutdownTimeout time.Duration,
) error {
	if ctx == nil ||
		server == nil ||
		workers == nil ||
		shutdownTimeout <= 0 {
		return fmt.Errorf(
			"invalid runtime lifecycle configuration",
		)
	}

	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	httpErrors := make(chan error, 1)
	workerErrors := make(chan error, 1)

	go func() {
		httpErrors <- server.ListenAndServe()
	}()
	go func() {
		workerErrors <- workers.Run(runContext)
	}()

	select {
	case httpErr := <-httpErrors:
		cancel()
		workerErr := <-workerErrors

		if ctx.Err() == nil &&
			(httpErr == nil ||
				errors.Is(
					httpErr,
					http.ErrServerClosed,
				)) {
			return errors.Join(
				ErrRuntimeComponentStopped,
				runtimeWorkerError(workerErr),
			)
		}
		return errors.Join(
			runtimeHTTPError(httpErr),
			runtimeWorkerError(workerErr),
		)

	case workerErr := <-workerErrors:
		cancel()
		shutdownErr := shutdownHTTP(
			server,
			shutdownTimeout,
		)
		httpErr := <-httpErrors

		if ctx.Err() == nil && workerErr == nil {
			return errors.Join(
				ErrRuntimeComponentStopped,
				shutdownErr,
				runtimeHTTPError(httpErr),
			)
		}
		return errors.Join(
			runtimeWorkerError(workerErr),
			shutdownErr,
			runtimeHTTPError(httpErr),
		)

	case <-ctx.Done():
		cancel()
		shutdownErr := shutdownHTTP(
			server,
			shutdownTimeout,
		)
		httpErr := <-httpErrors
		workerErr := <-workerErrors

		return errors.Join(
			runtimeWorkerError(workerErr),
			shutdownErr,
			runtimeHTTPError(httpErr),
		)
	}
}

func shutdownHTTP(
	server httpRuntime,
	shutdownTimeout time.Duration,
) error {
	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	shutdownErr := server.Shutdown(shutdownContext)
	cancel()
	if shutdownErr == nil {
		return nil
	}

	closeErr := server.Close()
	return errors.Join(
		fmt.Errorf(
			"graceful HTTP shutdown: %w",
			shutdownErr,
		),
		wrapRuntimeError(
			"force HTTP close",
			closeErr,
		),
	)
}

func runtimeHTTPError(err error) error {
	if err == nil ||
		errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve HTTP: %w", err)
}

func runtimeWorkerError(err error) error {
	return wrapRuntimeError("run workers", err)
}

func wrapRuntimeError(
	operation string,
	err error,
) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
