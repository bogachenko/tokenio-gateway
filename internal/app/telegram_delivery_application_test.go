package app

import (
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

type noNetworkRoundTripper func(*http.Request) (*http.Response, error)

func (f noNetworkRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}

func testTelegramInfrastructure(
	cfg config.Config,
) (TelegramInfrastructureGraph, error) {
	return newTelegramInfrastructureGraph(
		cfg,
		noNetworkRoundTripper(func(*http.Request) (*http.Response, error) {
			panic("unexpected Telegram HTTP call during graph construction")
		}),
	)
}

func TestApplicationGraphWiresTelegramDeliveryServicesOnlyWhenConfigured(
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
			TelegramInfrastructureGraph{},
			repositories,
		)
		if err != nil {
			t.Fatal(err)
		}
		if graph.TelegramDeliveryEnabled ||
			graph.TelegramDelivery != nil ||
			graph.TelegramRecovery != nil {
			t.Fatalf("disabled Telegram delivery graph = %+v", graph)
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
		cfg.TelegramAlertDedupePeriod = telegramHTTPTimeout

		telegramInfrastructure, err := testTelegramInfrastructure(cfg)
		if err != nil {
			t.Fatalf("newTelegramInfrastructureGraph: %v", err)
		}

		graph, err := NewApplicationGraph(
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
			t.Fatal(err)
		}
		if !graph.TelegramDeliveryEnabled ||
			graph.TelegramDelivery == nil ||
			graph.TelegramRecovery == nil {
			t.Fatalf("enabled Telegram delivery graph = %+v", graph)
		}
	})
}
