package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/config"
	telegramhttp "github.com/bogachenko/tokenio-gateway/internal/infrastructure/telegram/httpclient"
)

const telegramHTTPTimeout = 30 * time.Second

type TelegramInfrastructureGraph struct {
	Enabled bool
	Sender  telegramalert.MessageSender
}

func NewTelegramInfrastructureGraph(
	cfg config.Config,
) (TelegramInfrastructureGraph, error) {
	return newTelegramInfrastructureGraph(
		cfg,
		http.DefaultTransport,
	)
}

func newTelegramInfrastructureGraph(
	cfg config.Config,
	roundTripper http.RoundTripper,
) (TelegramInfrastructureGraph, error) {
	botToken := strings.TrimSpace(cfg.TelegramBotToken)
	chatID := strings.TrimSpace(cfg.TelegramChatID)

	switch {
	case botToken == "" && chatID == "":
		return TelegramInfrastructureGraph{}, nil
	case botToken == "" || chatID == "":
		return TelegramInfrastructureGraph{}, fmt.Errorf(
			"Telegram bot token and chat ID must be configured together",
		)
	case roundTripper == nil:
		return TelegramInfrastructureGraph{}, fmt.Errorf(
			"Telegram round tripper is nil",
		)
	}

	client, err := telegramhttp.New(telegramhttp.Config{
		BaseURL:              telegramhttp.DefaultBaseURL,
		BotToken:             botToken,
		ChatID:               chatID,
		RoundTripper:         roundTripper,
		Timeout:              telegramHTTPTimeout,
		MaxResponseBodyBytes: telegramhttp.DefaultMaxResponseBodyBytes,
	})
	if err != nil {
		return TelegramInfrastructureGraph{}, fmt.Errorf(
			"construct Telegram HTTP client: %w",
			err,
		)
	}

	graph := TelegramInfrastructureGraph{
		Enabled: true,
		Sender:  client,
	}
	if err := graph.Validate(); err != nil {
		return TelegramInfrastructureGraph{}, fmt.Errorf(
			"validate Telegram infrastructure graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g TelegramInfrastructureGraph) Validate() error {
	switch {
	case g.Enabled && g.Sender == nil:
		return fmt.Errorf("enabled Telegram sender is nil")
	case !g.Enabled && g.Sender != nil:
		return fmt.Errorf("disabled Telegram sender is non-nil")
	default:
		return nil
	}
}
