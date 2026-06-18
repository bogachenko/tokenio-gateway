package app

import (
	"context"
	"testing"
	"time"

	billingrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/billingrecovery"
	forwardingattemptrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/forwardingattemptrecovery"
)

type billingRecoveryObserverStub struct{}

func (billingRecoveryObserverStub) ObserveBillingRecoveryCycle(
	billingrecovery.Cycle,
) {
}

type forwardingRecoveryObserverStub struct{}

func (forwardingRecoveryObserverStub) ObserveForwardingAttemptRecoveryCycle(
	forwardingattemptrecovery.Cycle,
) {
}

func TestNewWorkerGraphWiresForwardingAttemptRecovery(
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

	graph, err := newWorkerGraphWithObservers(
		cfg,
		applications,
		workerGraphObserver{},
		forwardingRecoveryObserverStub{},
		billingRecoveryObserverStub{},
	)
	if err != nil {
		t.Fatalf("newWorkerGraphWithObservers: %v", err)
	}
	if !graph.ForwardingAttemptRecoveryEnabled ||
		graph.ForwardingAttemptRecovery == nil {
		t.Fatal(
			"forwarding attempt recovery worker is not wired",
		)
	}
}

type coordinatedWorkerRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func (r *coordinatedWorkerRunner) Run(
	ctx context.Context,
) error {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
	return nil
}

func TestWorkerGraphRunsBothWorkersUnderOneContext(
	t *testing.T,
) {
	first := &coordinatedWorkerRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	second := &coordinatedWorkerRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	graph := WorkerGraph{
		ProvisioningExpirationEnabled:    true,
		ProvisioningExpiration:           first,
		ForwardingAttemptRecoveryEnabled: true,
		ForwardingAttemptRecovery:        second,
	}
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	done := make(chan error, 1)
	go func() {
		done <- graph.Run(ctx)
	}()

	<-first.started
	<-second.started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker graph did not stop")
	}
	<-first.stopped
	<-second.stopped
}
