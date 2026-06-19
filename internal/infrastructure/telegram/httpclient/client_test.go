package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
)

func TestSendMessageDeliversExpectedTelegramRequest(t *testing.T) {
	var calls atomic.Int64
	const token = "secret-bot-token"
	const chatID = "-100123"
	const message = "reseller balance is low"

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			if request.Method != http.MethodPost {
				t.Fatalf("method = %s", request.Method)
			}
			if request.URL.Path != "/bot"+token+"/sendMessage" {
				t.Fatalf("path = %q", request.URL.Path)
			}
			var payload sendMessageRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload.ChatID != chatID || payload.Text != message {
				t.Fatalf("payload = %#v", payload)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"ok":true,"result":{"message_id":12345}}`)
		},
	))
	defer server.Close()

	client := mustClient(t, Config{
		BaseURL:              server.URL,
		BotToken:             token,
		ChatID:               chatID,
		RoundTripper:         server.Client().Transport,
		Timeout:              time.Second,
		MaxResponseBodyBytes: 1024,
	})

	result, err := client.SendMessage(context.Background(), message)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if result.Outcome != telegramalert.MessageDeliveryOutcomeResponseReceived ||
		result.TelegramMessageID != "12345" {
		t.Fatalf("result = %#v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if strings.Contains(client.String(), token) ||
		strings.Contains(client.GoString(), token) {
		t.Fatal("client string representation leaks token")
	}
}

func TestSendMessageClassifiesDefinitiveResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{
			name:    "HTTP rejection",
			status:  http.StatusBadGateway,
			body:    `{"ok":false}`,
			wantErr: ErrHTTPStatus,
		},
		{
			name:    "Telegram rejection",
			status:  http.StatusOK,
			body:    `{"ok":false}`,
			wantErr: ErrDeliveryRejected,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(test.status)
					_, _ = io.WriteString(writer, test.body)
				},
			))
			defer server.Close()

			client := mustClient(t, Config{
				BaseURL:              server.URL,
				BotToken:             "secret",
				ChatID:               "chat",
				RoundTripper:         server.Client().Transport,
				Timeout:              time.Second,
				MaxResponseBodyBytes: 1024,
			})
			result, err := client.SendMessage(
				context.Background(),
				"message",
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v", err)
			}
			if result.Outcome !=
				telegramalert.MessageDeliveryOutcomeResponseReceived {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSendMessageClassifiesUnknownDeliveryOutcome(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		limit   int64
		wantErr error
	}{
		{
			name:    "invalid JSON",
			body:    `{`,
			limit:   1024,
			wantErr: ErrInvalidResponse,
		},
		{
			name:    "unknown response field",
			body:    `{"ok":true,"unexpected":1}`,
			limit:   1024,
			wantErr: ErrInvalidResponse,
		},
		{
			name:    "response too large",
			body:    strings.Repeat("x", 17),
			limit:   16,
			wantErr: ErrResponseTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(writer, test.body)
				},
			))
			defer server.Close()
			client := mustClient(t, Config{
				BaseURL:              server.URL,
				BotToken:             "secret",
				ChatID:               "chat",
				RoundTripper:         server.Client().Transport,
				Timeout:              time.Second,
				MaxResponseBodyBytes: test.limit,
			})
			result, err := client.SendMessage(
				context.Background(),
				"message",
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v", err)
			}
			if result.Outcome !=
				telegramalert.MessageDeliveryOutcomeSentNoResponse {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSendMessageUsesBoundedContext(t *testing.T) {
	roundTripper := roundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		},
	)
	client := mustClient(t, Config{
		BaseURL:              "https://api.telegram.org",
		BotToken:             "secret",
		ChatID:               "chat",
		RoundTripper:         roundTripper,
		Timeout:              20 * time.Millisecond,
		MaxResponseBodyBytes: 1024,
	})

	startedAt := time.Now()
	result, err := client.SendMessage(
		context.Background(),
		"message",
	)
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("error = %v", err)
	}
	if result.Outcome != telegramalert.MessageDeliveryOutcomeSentNoResponse {
		t.Fatalf("result = %#v", result)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("bounded request took %s", elapsed)
	}
}

func TestSendMessageRejectsInvalidInputAsNotSent(t *testing.T) {
	client := mustClient(t, Config{
		BaseURL:              DefaultBaseURL,
		BotToken:             "secret",
		ChatID:               "chat",
		RoundTripper:         http.DefaultTransport,
		Timeout:              time.Second,
		MaxResponseBodyBytes: 1024,
	})

	result, err := client.SendMessage(nil, "message")
	if !errors.Is(err, ErrInvalidMessage) ||
		result.Outcome != telegramalert.MessageDeliveryOutcomeNotSent {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	result, err = client.SendMessage(context.Background(), " ")
	if !errors.Is(err, ErrInvalidMessage) ||
		result.Outcome != telegramalert.MessageDeliveryOutcomeNotSent {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	valid := Config{
		BaseURL:              "https://api.telegram.org",
		BotToken:             "secret",
		ChatID:               "chat",
		RoundTripper:         http.DefaultTransport,
		Timeout:              time.Second,
		MaxResponseBodyBytes: 1024,
	}
	tests := []func(*Config){
		func(value *Config) { value.BaseURL = "://" },
		func(value *Config) {
			value.BaseURL = "https://user:pass@example.com"
		},
		func(value *Config) { value.BotToken = " " },
		func(value *Config) { value.ChatID = " " },
		func(value *Config) { value.RoundTripper = nil },
		func(value *Config) { value.Timeout = 0 },
		func(value *Config) { value.MaxResponseBodyBytes = 0 },
	}
	for _, mutate := range tests {
		config := valid
		mutate(&config)
		if _, err := New(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("error = %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}

func mustClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}
