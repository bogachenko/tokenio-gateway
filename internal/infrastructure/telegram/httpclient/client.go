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
	"strconv"
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
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil ||
		baseURL.Scheme == "" ||
		baseURL.Host == "" ||
		baseURL.User != nil ||
		(baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		strings.TrimSpace(config.BotToken) == "" ||
		strings.TrimSpace(config.ChatID) == "" ||
		config.RoundTripper == nil ||
		config.Timeout <= 0 ||
		config.MaxResponseBodyBytes <= 0 {
		return nil, ErrInvalidConfig
	}
	copyURL := *baseURL
	return &Client{
		baseURL:              &copyURL,
		botToken:             config.BotToken,
		chatID:               config.ChatID,
		roundTripper:         config.RoundTripper,
		timeout:              config.Timeout,
		maxResponseBodyBytes: config.MaxResponseBodyBytes,
	}, nil
}

func (c *Client) String() string {
	if c == nil {
		return "telegram.httpclient.Client<nil>"
	}
	return "telegram.httpclient.Client"
}

func (c *Client) GoString() string {
	return c.String()
}

func (c *Client) SendMessage(
	ctx context.Context,
	message string,
) (telegramalert.MessageDeliveryResult, error) {
	if c == nil ||
		c.baseURL == nil ||
		c.roundTripper == nil ||
		c.timeout <= 0 ||
		c.maxResponseBodyBytes <= 0 {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeNotSent},
			ErrInvalidConfig
	}
	if ctx == nil || strings.TrimSpace(message) == "" {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeNotSent},
			ErrInvalidMessage
	}

	requestBody, err := json.Marshal(sendMessageRequest{
		ChatID: c.chatID,
		Text:   message,
	})
	if err != nil {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeNotSent},
			fmt.Errorf("%w: encode request", ErrInvalidMessage)
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
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeNotSent},
			fmt.Errorf("%w: construct request", ErrTransport)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.roundTripper.RoundTrip(request)
	if err != nil {
		closeResponseBody(response)
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeSentNoResponse},
			fmt.Errorf("%w: round trip", ErrTransport)
	}
	if response == nil || response.Body == nil {
		closeResponseBody(response)
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeSentNoResponse},
			fmt.Errorf("%w: missing response", ErrInvalidResponse)
	}
	defer response.Body.Close()

	body, err := readLimited(
		response.Body,
		c.maxResponseBodyBytes,
	)
	if err != nil {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeSentNoResponse}, err
	}
	if response.StatusCode < http.StatusOK ||
		response.StatusCode > 299 {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeResponseReceived},
			fmt.Errorf(
				"%w: status %d",
				ErrHTTPStatus,
				response.StatusCode,
			)
	}

	var payload sendMessageResponse
	if err := decodeSingleJSON(body, &payload); err != nil {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeSentNoResponse},
			fmt.Errorf("%w: decode body", ErrInvalidResponse)
	}
	if !payload.OK {
		return telegramalert.MessageDeliveryResult{Outcome: telegramalert.MessageDeliveryOutcomeResponseReceived},
			ErrDeliveryRejected
	}
	messageID := ""
	if payload.Result.MessageID > 0 {
		messageID = strconv.FormatInt(payload.Result.MessageID, 10)
	}
	return telegramalert.MessageDeliveryResult{
		Outcome:           telegramalert.MessageDeliveryOutcomeResponseReceived,
		TelegramMessageID: messageID,
	}, nil
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
	OK     bool `json:"ok"`
	Result struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
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
