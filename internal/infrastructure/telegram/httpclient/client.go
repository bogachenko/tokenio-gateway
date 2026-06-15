package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
)

const (
	DefaultBaseURL              = "https://api.telegram.org"
	DefaultMaxResponseBodyBytes = int64(64 << 10)
)

var (
	ErrInvalidConfig    = errors.New("invalid Telegram client config")
	ErrInvalidMessage   = errors.New("invalid Telegram message")
	ErrTransport        = errors.New("Telegram transport error")
	ErrHTTPStatus       = errors.New("Telegram HTTP status")
	ErrInvalidResponse  = errors.New("invalid Telegram response")
	ErrResponseTooLarge = errors.New("Telegram response too large")
	ErrDeliveryRejected = errors.New("Telegram delivery rejected")
)

type Config struct {
	BaseURL              string
	BotToken             string
	ChatID               string
	RoundTripper         http.RoundTripper
	Timeout              time.Duration
	MaxResponseBodyBytes int64
}

type Client struct {
	baseURL              *url.URL
	botToken             string
	chatID               string
	roundTripper         http.RoundTripper
	timeout              time.Duration
	maxResponseBodyBytes int64
}

var _ telegramalert.MessageSender = (*Client)(nil)

func New(config Config) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: base URL", ErrInvalidConfig)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	if strings.TrimSpace(config.BotToken) == "" {
		return nil, fmt.Errorf("%w: bot token is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(config.ChatID) == "" {
		return nil, fmt.Errorf("%w: chat ID is required", ErrInvalidConfig)
	}
	if config.RoundTripper == nil {
		return nil, fmt.Errorf("%w: round tripper is required", ErrInvalidConfig)
	}
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout is required", ErrInvalidConfig)
	}
	if config.MaxResponseBodyBytes <= 0 {
		return nil, fmt.Errorf(
			"%w: max response body bytes is required",
			ErrInvalidConfig,
		)
	}

	return &Client{
		baseURL:              parsed,
		botToken:             config.BotToken,
		chatID:               config.ChatID,
		roundTripper:         config.RoundTripper,
		timeout:              config.Timeout,
		maxResponseBodyBytes: config.MaxResponseBodyBytes,
	}, nil
}

func (c *Client) String() string {
	return "Telegram HTTP client"
}

func (c *Client) GoString() string {
	return "Telegram HTTP client"
}

func (c *Client) SendMessage(
	ctx context.Context,
	message string,
) error {
	if c == nil ||
		c.baseURL == nil ||
		c.roundTripper == nil ||
		c.timeout <= 0 ||
		c.maxResponseBodyBytes <= 0 {
		return ErrInvalidConfig
	}
	if ctx == nil || strings.TrimSpace(message) == "" {
		return ErrInvalidMessage
	}

	requestBody, err := json.Marshal(sendMessageRequest{
		ChatID: c.chatID,
		Text:   message,
	})
	if err != nil {
		return fmt.Errorf("%w: encode request", ErrInvalidMessage)
	}

	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		c.endpoint(),
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return fmt.Errorf("%w: construct request", ErrTransport)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.roundTripper.RoundTrip(request)
	if err != nil {
		closeResponseBody(response)
		return fmt.Errorf("%w: round trip", ErrTransport)
	}
	if response == nil || response.Body == nil {
		closeResponseBody(response)
		return fmt.Errorf("%w: missing response", ErrInvalidResponse)
	}
	defer response.Body.Close()

	body, err := readLimited(
		response.Body,
		c.maxResponseBodyBytes,
	)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK ||
		response.StatusCode > 299 {
		return fmt.Errorf(
			"%w: status %d",
			ErrHTTPStatus,
			response.StatusCode,
		)
	}

	var payload sendMessageResponse
	if err := decodeSingleJSON(body, &payload); err != nil {
		return fmt.Errorf("%w: decode body", ErrInvalidResponse)
	}
	if !payload.OK {
		return ErrDeliveryRejected
	}
	return nil
}

func (c *Client) endpoint() string {
	copy := *c.baseURL
	copy.Path = strings.TrimRight(copy.Path, "/") +
		"/bot" + url.PathEscape(c.botToken) + "/sendMessage"
	return copy.String()
}

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type sendMessageResponse struct {
	OK bool `json:"ok"`
}

func readLimited(
	reader io.Reader,
	limit int64,
) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: read body", ErrTransport)
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}

func decodeSingleJSON(
	body []byte,
	target any,
) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func closeResponseBody(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}
