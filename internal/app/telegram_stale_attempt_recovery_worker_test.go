package app

import "testing"

func TestNewWorkerGraphWiresTelegramStaleAttemptRecovery(
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
	if applications.TelegramStaleAttemptRecovery == nil {
		t.Fatal("Telegram stale-attempt recovery service is not wired")
	}

	graph, err := NewWorkerGraph(
		cfg,
		applications,
		validLoggingGraph(t),
		workerGraphObserver{},
	)
	if err != nil {
		t.Fatalf("NewWorkerGraph: %v", err)
	}
	if !graph.TelegramStaleAttemptRecoveryEnabled ||
		graph.TelegramStaleAttemptRecovery == nil {
		t.Fatal("Telegram stale-attempt recovery worker is not wired")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestWorkerGraphValidateRejectsTelegramRecoveryMixedState(
	t *testing.T,
) {
	enabledWithoutWorker := WorkerGraph{
		TelegramStaleAttemptRecoveryEnabled: true,
	}
	if err := enabledWithoutWorker.Validate(); err == nil {
		t.Fatal("expected enabled Telegram recovery without worker to fail")
	}

	disabledWithWorker := WorkerGraph{
		TelegramStaleAttemptRecovery: &workerGraphRunner{},
	}
	if err := disabledWithWorker.Validate(); err == nil {
		t.Fatal("expected disabled Telegram recovery with worker to fail")
	}
}
