package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/config"
)

type telegramInfrastructureRoundTripper struct{}

func (telegramInfrastructureRoundTripper) RoundTrip(
	*http.Request,
) (*http.Response, error) {
	return nil, errors.New("unused")
}

func TestTelegramInfrastructureGraphDisabledWithoutCredentials(
	t *testing.T,
) {
	graph, err := newTelegramInfrastructureGraph(
		config.Config{},
		telegramInfrastructureRoundTripper{},
	)
	if err != nil {
		t.Fatalf("newTelegramInfrastructureGraph: %v", err)
	}
	if graph.Enabled || graph.Sender != nil {
		t.Fatalf("disabled graph = %+v", graph)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestTelegramInfrastructureGraphRejectsPartialCredentials(
	t *testing.T,
) {
	tests := []config.Config{
		{TelegramBotToken: "bot-token"},
		{TelegramChatID: "chat-id"},
	}

	for _, cfg := range tests {
		if _, err := newTelegramInfrastructureGraph(
			cfg,
			telegramInfrastructureRoundTripper{},
		); err == nil {
			t.Fatalf("expected partial configuration error: %+v", cfg)
		}
	}
}

func TestTelegramInfrastructureGraphEnabledWithCompleteCredentials(
	t *testing.T,
) {
	graph, err := newTelegramInfrastructureGraph(
		config.Config{
			TelegramBotToken: "bot-token",
			TelegramChatID:   "chat-id",
		},
		telegramInfrastructureRoundTripper{},
	)
	if err != nil {
		t.Fatalf("newTelegramInfrastructureGraph: %v", err)
	}
	if !graph.Enabled || graph.Sender == nil {
		t.Fatalf("enabled graph = %+v", graph)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestTelegramInfrastructureGraphValidateRejectsMixedState(
	t *testing.T,
) {
	enabledWithoutSender := TelegramInfrastructureGraph{
		Enabled: true,
	}
	if err := enabledWithoutSender.Validate(); err == nil {
		t.Fatal("expected enabled graph without sender to fail")
	}

	disabledWithSender := TelegramInfrastructureGraph{
		Sender: &telegramInfrastructureSenderStub{},
	}
	if err := disabledWithSender.Validate(); err == nil {
		t.Fatal("expected disabled graph with sender to fail")
	}
}

type telegramInfrastructureSenderStub struct{}

func (*telegramInfrastructureSenderStub) SendMessage(
	_ context.Context,
	_ string,
) (telegramalert.MessageDeliveryResult, error) {
	return telegramalert.MessageDeliveryResult{
		Outcome: telegramalert.MessageDeliveryOutcomeNotSent,
	}, nil
}
