package app

import (
	"testing"
	"time"
)

func TestNewWorkerGraphWiresBillingRecovery(t *testing.T) {
	cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)

	cfg.BillingRecoveryInterval = time.Minute
	cfg.BillingRecoveryBatchSize = 100

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
	if applications.BillingRecovery == nil {
		t.Fatal("billing recovery application service is not wired")
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
	if !graph.BillingRecoveryEnabled || graph.BillingRecovery == nil {
		t.Fatal("billing recovery worker is not wired")
	}
}
