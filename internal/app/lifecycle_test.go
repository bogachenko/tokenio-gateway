package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type lifecycleHTTP struct {
	serveErr    error
	shutdownErr error
	closeErr    error

	started chan struct{}
	stopped chan struct{}
	once    sync.Once

	mu            sync.Mutex
	shutdownCalls int
	closeCalls    int
}

func newLifecycleHTTP() *lifecycleHTTP {
	return &lifecycleHTTP{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (s *lifecycleHTTP) ListenAndServe() error {
	close(s.started)
	if s.serveErr != nil {
		return s.serveErr
	}
	<-s.stopped
	return http.ErrServerClosed
}

func (s *lifecycleHTTP) Shutdown(
	context.Context,
) error {
	s.mu.Lock()
	s.shutdownCalls++
	s.mu.Unlock()

	if s.shutdownErr == nil {
		s.once.Do(func() {
			close(s.stopped)
		})
	}
	return s.shutdownErr
}

func (s *lifecycleHTTP) Close() error {
	s.mu.Lock()
	s.closeCalls++
	s.mu.Unlock()

	s.once.Do(func() {
		close(s.stopped)
	})
	return s.closeErr
}

type lifecycleWorkers struct {
	err               error
	returnImmediately bool

	started chan struct{}
	stopped chan struct{}
}

func newLifecycleWorkers() *lifecycleWorkers {
	return &lifecycleWorkers{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (w *lifecycleWorkers) Run(
	ctx context.Context,
) error {
	close(w.started)
	if w.returnImmediately {
		close(w.stopped)
		return w.err
	}
	<-ctx.Done()
	close(w.stopped)
	return w.err
}

func TestServeRuntimeCancelsWorkersAndShutsDownHTTP(
	t *testing.T,
) {
	server := newLifecycleHTTP()
	workers := newLifecycleWorkers()
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	done := make(chan error, 1)
	go func() {
		done <- serveRuntime(
			ctx,
			server,
			workers,
			time.Second,
		)
	}()

	<-server.started
	<-workers.started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveRuntime: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}

	server.mu.Lock()
	shutdownCalls := server.shutdownCalls
	closeCalls := server.closeCalls
	server.mu.Unlock()
	if shutdownCalls != 1 || closeCalls != 0 {
		t.Fatalf(
			"shutdown calls=%d close calls=%d",
			shutdownCalls,
			closeCalls,
		)
	}
	select {
	case <-workers.stopped:
	default:
		t.Fatal("workers were not stopped")
	}
}

func TestServeRuntimeWorkerFailureStopsHTTP(
	t *testing.T,
) {
	workerErr := errors.New("worker failed")
	server := newLifecycleHTTP()
	workers := newLifecycleWorkers()
	workers.returnImmediately = true
	workers.err = workerErr

	err := serveRuntime(
		context.Background(),
		server,
		workers,
		time.Second,
	)
	if !errors.Is(err, workerErr) {
		t.Fatalf(
			"error = %v, want worker error",
			err,
		)
	}

	server.mu.Lock()
	shutdownCalls := server.shutdownCalls
	server.mu.Unlock()
	if shutdownCalls != 1 {
		t.Fatalf(
			"shutdown calls = %d, want 1",
			shutdownCalls,
		)
	}
}

func TestServeRuntimeHTTPFailureCancelsWorkers(
	t *testing.T,
) {
	serveErr := errors.New("listen failed")
	server := newLifecycleHTTP()
	server.serveErr = serveErr
	workers := newLifecycleWorkers()

	err := serveRuntime(
		context.Background(),
		server,
		workers,
		time.Second,
	)
	if !errors.Is(err, serveErr) {
		t.Fatalf(
			"error = %v, want HTTP error",
			err,
		)
	}
	select {
	case <-workers.stopped:
	default:
		t.Fatal(
			"HTTP failure did not cancel workers",
		)
	}
}

func TestServeRuntimeRejectsUnexpectedCleanWorkerExit(
	t *testing.T,
) {
	server := newLifecycleHTTP()
	workers := newLifecycleWorkers()
	workers.returnImmediately = true

	err := serveRuntime(
		context.Background(),
		server,
		workers,
		time.Second,
	)
	if !errors.Is(
		err,
		ErrRuntimeComponentStopped,
	) {
		t.Fatalf(
			"error = %v, want ErrRuntimeComponentStopped",
			err,
		)
	}
}

func TestServeRuntimeForcesCloseAfterShutdownFailure(
	t *testing.T,
) {
	shutdownErr := errors.New("shutdown failed")
	server := newLifecycleHTTP()
	server.shutdownErr = shutdownErr
	workers := newLifecycleWorkers()
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	done := make(chan error, 1)
	go func() {
		done <- serveRuntime(
			ctx,
			server,
			workers,
			time.Second,
		)
	}()
	<-server.started
	<-workers.started
	cancel()

	err := <-done
	if !errors.Is(err, shutdownErr) {
		t.Fatalf(
			"error = %v, want shutdown error",
			err,
		)
	}

	server.mu.Lock()
	closeCalls := server.closeCalls
	server.mu.Unlock()
	if closeCalls != 1 {
		t.Fatalf(
			"close calls = %d, want 1",
			closeCalls,
		)
	}
}
