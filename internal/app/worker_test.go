package app

import (
	"context"
	"errors"
	"testing"
	"time"

	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
)

type workerGraphObserver struct{}

func (workerGraphObserver) ObserveProvisioningExpirationCycle(
	provisioningexpiration.Cycle,
) {
}

type workerGraphRunner struct {
	calls int
}

func (w *workerGraphRunner) Run(
	ctx context.Context,
) error {
	w.calls++
	<-ctx.Done()
	return nil
}

func TestNewWorkerGraphWiresProvisioningExpiration(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.APIKeyProvisioningExpirationInterval =
		time.Minute
	cfg.APIKeyProvisioningExpirationBatchSize = 100

	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		TelegramInfrastructureGraph{},
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}

	graph, err := NewWorkerGraph(
		cfg,
		applications,
		workerGraphObserver{},
	)
	if err != nil {
		t.Fatalf("NewWorkerGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("worker graph: %v", err)
	}
	if !graph.ProvisioningExpirationEnabled ||
		graph.ProvisioningExpiration == nil {
		t.Fatal(
			"provisioning expiration worker is not wired",
		)
	}
}

func TestNewWorkerGraphAllowsProvisioningDisabled(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)

	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		TelegramInfrastructureGraph{},
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}
	applications.ProvisioningEnabled = false
	applications.Provisioning = nil
	if err := applications.Validate(); err != nil {
		t.Fatalf(
			"disabled application graph: %v",
			err,
		)
	}

	graph, err := NewWorkerGraph(
		cfg,
		applications,
		nil,
	)
	if err != nil {
		t.Fatalf("NewWorkerGraph: %v", err)
	}
	if graph.ProvisioningExpirationEnabled ||
		graph.ProvisioningExpiration != nil {
		t.Fatal(
			"disabled worker graph contains worker",
		)
	}
}

func TestNewWorkerGraphRejectsInvalidWorkerConfig(
	t *testing.T,
) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)
	cfg.APIKeyProvisioningExpirationInterval = 0
	cfg.APIKeyProvisioningExpirationBatchSize = 100

	applications, err := NewApplicationGraph(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		TelegramInfrastructureGraph{},
		repositories,
	)
	if err != nil {
		t.Fatalf("NewApplicationGraph: %v", err)
	}

	graph, err := NewWorkerGraph(
		cfg,
		applications,
		workerGraphObserver{},
	)
	if err == nil {
		t.Fatal("expected worker construction error")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf(
			"zero graph after construction failure must remain valid: %v",
			err,
		)
	}
}

func TestWorkerGraphRunDisabledWaitsForCancellation(
	t *testing.T,
) {
	graph := WorkerGraph{}
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	done := make(chan error, 1)
	go func() {
		done <- graph.Run(ctx)
	}()

	select {
	case err := <-done:
		t.Fatalf(
			"disabled graph returned before cancellation: %v",
			err,
		)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal(
			"disabled graph did not stop after cancellation",
		)
	}
}

func TestWorkerGraphRunRejectsInvalidState(
	t *testing.T,
) {
	runner := &workerGraphRunner{}
	graph := WorkerGraph{
		ProvisioningExpiration: runner,
	}

	err := graph.Run(context.Background())
	if !errors.Is(err, ErrInvalidWorkerGraph) {
		t.Fatalf(
			"error = %v, want ErrInvalidWorkerGraph",
			err,
		)
	}
	if runner.calls != 0 {
		t.Fatal("invalid graph invoked worker")
	}
}
