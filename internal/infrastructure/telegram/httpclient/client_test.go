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
			if request.URL.Path !=
				"/bot"+token+"/sendMessage" {
				t.Fatalf("path = %q", request.URL.Path)
			}
			if request.URL.RawQuery != "" {
				t.Fatalf("query = %q", request.URL.RawQuery)
			}
			if request.Header.Get("Content-Type") !=
				"application/json" {
				t.Fatalf(
					"content type = %q",
					request.Header.Get("Content-Type"),
				)
			}

			var payload sendMessageRequest
			decoder := json.NewDecoder(request.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if payload.ChatID != chatID ||
				payload.Text != message {
				t.Fatalf("payload = %#v", payload)
			}

			writer.Header().Set(
				"Content-Type",
				"application/json",
			)
			_, _ = io.WriteString(writer, `{"ok":true}`)
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

	if err := client.SendMessage(
		context.Background(),
		message,
	); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if strings.Contains(client.String(), token) ||
		strings.Contains(client.GoString(), token) {
		t.Fatal("client string representation leaks token")
	}
}

func TestSendMessageRejectsRedirectWithoutFollowingIt(t *testing.T) {
	var redirectedCalls atomic.Int64
	redirected := httptest.NewServer(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			redirectedCalls.Add(1)
		},
	))
	defer redirected.Close()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(
				writer,
				request,
				redirected.URL,
				http.StatusFound,
			)
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

	err := client.SendMessage(
		context.Background(),
		"message",
	)
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("error = %v, want HTTP status", err)
	}
	if redirectedCalls.Load() != 0 {
		t.Fatalf(
			"redirected calls = %d, want 0",
			redirectedCalls.Load(),
		)
	}
}

func TestSendMessageValidatesTelegramResponse(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		limit   int64
		wantErr error
	}{
		{
			name:    "non-success HTTP status",
			status:  http.StatusBadGateway,
			body:    `{"ok":false}`,
			limit:   1024,
			wantErr: ErrHTTPStatus,
		},
		{
			name:    "Telegram rejection",
			status:  http.StatusOK,
			body:    `{"ok":false}`,
			limit:   1024,
			wantErr: ErrDeliveryRejected,
		},
		{
			name:    "invalid JSON",
			status:  http.StatusOK,
			body:    `{`,
			limit:   1024,
			wantErr: ErrInvalidResponse,
		},
		{
			name:    "unknown response field",
			status:  http.StatusOK,
			body:    `{"ok":true,"unexpected":1}`,
			limit:   1024,
			wantErr: ErrInvalidResponse,
		},
		{
			name:    "trailing JSON",
			status:  http.StatusOK,
			body:    `{"ok":true} {}`,
			limit:   1024,
			wantErr: ErrInvalidResponse,
		},
		{
			name:    "response too large",
			status:  http.StatusOK,
			body:    strings.Repeat("x", 17),
			limit:   16,
			wantErr: ErrResponseTooLarge,
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
				MaxResponseBodyBytes: test.limit,
			})

			err := client.SendMessage(
				context.Background(),
				"message",
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf(
					"error = %v, want %v",
					err,
					test.wantErr,
				)
			}
			if strings.Contains(
				err.Error(),
				"secret",
			) {
				t.Fatalf("error leaks token: %v", err)
			}
		})
	}
}

func TestSendMessageUsesBoundedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
			writer.WriteHeader(http.StatusGatewayTimeout)
		},
	))
	defer server.Close()

	client := mustClient(t, Config{
		BaseURL:              server.URL,
		BotToken:             "secret",
		ChatID:               "chat",
		RoundTripper:         server.Client().Transport,
		Timeout:              20 * time.Millisecond,
		MaxResponseBodyBytes: 1024,
	})

	err := client.SendMessage(
		context.Background(),
		"message",
	)
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("error = %v, want transport", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaks token: %v", err)
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

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "invalid base URL",
			mutate: func(value *Config) {
				value.BaseURL = "://"
			},
		},
		{
			name: "base URL credentials",
			mutate: func(value *Config) {
				value.BaseURL = "https://user:pass@example.com"
			},
		},
		{
			name: "blank token",
			mutate: func(value *Config) {
				value.BotToken = " "
			},
		},
		{
			name: "blank chat ID",
			mutate: func(value *Config) {
				value.ChatID = " "
			},
		},
		{
			name: "nil round tripper",
			mutate: func(value *Config) {
				value.RoundTripper = nil
			},
		},
		{
			name: "nonpositive timeout",
			mutate: func(value *Config) {
				value.Timeout = 0
			},
		},
		{
			name: "nonpositive response limit",
			mutate: func(value *Config) {
				value.MaxResponseBodyBytes = 0
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			_, err := New(config)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf(
					"error = %v, want invalid config",
					err,
				)
			}
			if err != nil &&
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaks token: %v", err)
			}
		})
	}
}

func TestSendMessageRejectsInvalidInput(t *testing.T) {
	client := mustClient(t, Config{
		BaseURL:              DefaultBaseURL,
		BotToken:             "secret",
		ChatID:               "chat",
		RoundTripper:         http.DefaultTransport,
		Timeout:              time.Second,
		MaxResponseBodyBytes: 1024,
	})

	if err := client.SendMessage(nil, "message"); !errors.Is(
		err,
		ErrInvalidMessage,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := client.SendMessage(
		context.Background(),
		" ",
	); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("blank message error = %v", err)
	}
}

func mustClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}
