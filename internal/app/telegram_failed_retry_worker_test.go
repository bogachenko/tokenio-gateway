package app

import (
	"testing"
	"time"
)

func TestNewWorkerGraphWiresTelegramFailedRetryOnlyWhenConfigured(
	t *testing.T,
) {
	t.Run("disabled", func(t *testing.T) {
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

		graph, err := NewWorkerGraph(
			cfg,
			applications,
			validLoggingGraph(t),
			workerGraphObserver{},
		)
		if err != nil {
			t.Fatalf("NewWorkerGraph: %v", err)
		}
		if graph.TelegramFailedRetryEnabled ||
			graph.TelegramFailedRetry != nil {
			t.Fatalf("disabled Telegram failed-retry worker = %+v", graph)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		cfg,
			primitives,
			security,
			provisioningInfrastructure,
			billingInfrastructure,
			forwardingInfrastructure,
			repositories := validApplicationGraphInputs(t)
		cfg.TelegramBotToken = "bot-token"
		cfg.TelegramChatID = "chat-id"
		cfg.TelegramAlertDedupePeriod = time.Hour
		cfg.ResellerBalanceAlertCents = 10_000
		telegramInfrastructure, err := testTelegramInfrastructure(cfg)
		if err != nil {
			t.Fatalf("testTelegramInfrastructure: %v", err)
		}

		applications, err := NewApplicationGraph(
			cfg,
			primitives,
			security,
			provisioningInfrastructure,
			billingInfrastructure,
			forwardingInfrastructure,
			telegramInfrastructure,
			repositories,
		)
		if err != nil {
			t.Fatalf("NewApplicationGraph: %v", err)
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
		if !graph.TelegramFailedRetryEnabled ||
			graph.TelegramFailedRetry == nil {
			t.Fatalf("enabled Telegram failed-retry worker = %+v", graph)
		}
	})
}
