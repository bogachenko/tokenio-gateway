package app

import (
	"testing"
	"time"
)

func TestApplicationGraphWiresTelegramBalanceAlertsOnlyWhenConfigured(
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

		graph, err := NewApplicationGraph(
			cfg,
			primitives,
			security,
			provisioningInfrastructure,
			billingInfrastructure,
			forwardingInfrastructure,
			TelegramInfrastructureGraph{}, validLoggingGraph(t),

			repositories,
		)
		if err != nil {
			t.Fatal(err)
		}
		if graph.TelegramAlertsEnabled || graph.TelegramAlerts != nil {
			t.Fatalf("disabled Telegram alerts = %+v", graph.TelegramAlerts)
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
		telegramInfrastructure, err := testTelegramInfrastructure(cfg)
		if err != nil {
			t.Fatal(err)
		}

		graph, err := NewApplicationGraph(
			cfg,
			primitives,
			security,
			provisioningInfrastructure,
			billingInfrastructure,
			forwardingInfrastructure,
			telegramInfrastructure, validLoggingGraph(t),

			repositories,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !graph.TelegramAlertsEnabled || graph.TelegramAlerts == nil {
			t.Fatal("enabled Telegram alert service is not wired")
		}
	})
}
