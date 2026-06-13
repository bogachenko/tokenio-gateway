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

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const currencyRUB = "RUB"

const DefaultMaxResponseBodyBytes int64 = 1 << 20

var (
	ErrInvalidConfig     = errors.New("invalid billing http client config")
	ErrInvalidRequest    = errors.New("invalid billing request")
	ErrBillingHTTPStatus = errors.New("billing http status")
	ErrBillingTransport  = errors.New("billing transport error")
	ErrInvalidResponse   = errors.New("invalid billing response")
)

type Config struct {
	BaseURL              string
	ServiceToken         string
	RoundTripper         http.RoundTripper
	Timeout              time.Duration
	MaxResponseBodyBytes int64
}

type Client struct {
	baseURL              *url.URL
	serviceToken         string
	roundTripper         http.RoundTripper
	timeout              time.Duration
	maxResponseBodyBytes int64
}

func New(cfg Config) (*Client, error) {
	base, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ServiceToken) == "" {
		return nil, fmt.Errorf("%w: service token is required", ErrInvalidConfig)
	}
	if cfg.RoundTripper == nil {
		return nil, fmt.Errorf("%w: round tripper is required", ErrInvalidConfig)
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout is required", ErrInvalidConfig)
	}
	limit := cfg.MaxResponseBodyBytes
	if limit <= 0 {
		return nil, fmt.Errorf("%w: max response body bytes is required", ErrInvalidConfig)
	}
	return &Client{
		baseURL:              base,
		serviceToken:         cfg.ServiceToken,
		roundTripper:         cfg.RoundTripper,
		timeout:              cfg.Timeout,
		maxResponseBodyBytes: limit,
	}, nil
}

func (c *Client) String() string { return "billing http client" }

func (c *Client) GoString() string { return "billing http client" }

func (c *Client) GetBalance(ctx context.Context, billingToken string) (ports.BillingBalance, error) {
	var out ports.BillingBalance
	if strings.TrimSpace(billingToken) == "" {
		return out, fmt.Errorf("%w: billing token is required", ErrInvalidRequest)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestContext,
		http.MethodGet,
		c.endpoint("/api/v1/wallet/balance"),
		nil,
	)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+billingToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.roundTripper.RoundTrip(req)
	if err != nil {
		closeResponseBody(resp)
		return out, fmt.Errorf("%w: round trip", ErrBillingTransport)
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, c.maxResponseBodyBytes)
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, fmt.Errorf("%w: status %d", ErrBillingHTTPStatus, resp.StatusCode)
	}
	var dto balanceResponse
	if err := decodeSingleJSON(body, &dto); err != nil {
		return out, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if dto.Currency != currencyRUB {
		return out, fmt.Errorf("%w: currency", ErrInvalidResponse)
	}
	if dto.BalanceCents < 0 {
		return out, fmt.Errorf("%w: balance", ErrInvalidResponse)
	}
	return ports.BillingBalance{Currency: dto.Currency, BalanceCents: dto.BalanceCents}, nil
}

func (c *Client) Charge(ctx context.Context, request ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	var out ports.BillingChargeResult
	if err := validateChargeRequest(request); err != nil {
		return out, err
	}
	dto := chargeRequest{
		RequestID:    request.RequestID,
		UserID:       request.UserID,
		Model:        request.Model,
		InputTokens:  request.InputTokens,
		OutputTokens: request.OutputTokens,
		AmountCents:  request.AmountCents,
		Currency:     request.Currency,
	}
	body, err := json.Marshal(dto)
	if err != nil {
		return out, err
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		c.endpoint("/api/v1/usage/charge"),
		bytes.NewReader(body),
	)
	if err != nil {
		return out, err
	}
	req.Header.Set("X-Service-Token", c.serviceToken)
	req.Header.Set("Idempotency-Key", request.RequestID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.roundTripper.RoundTrip(req)
	if err != nil {
		closeResponseBody(resp)
		return out, fmt.Errorf("%w: round trip", ErrBillingTransport)
	}
	defer resp.Body.Close()
	respBody, err := readLimited(resp.Body, c.maxResponseBodyBytes)
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, fmt.Errorf("%w: status %d", ErrBillingHTTPStatus, resp.StatusCode)
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return out, nil
	}
	var dtoResp chargeResponse
	if err := decodeSingleJSON(respBody, &dtoResp); err != nil {
		return out, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if dtoResp.BalanceCents != nil && *dtoResp.BalanceCents < 0 {
		return out, fmt.Errorf("%w: balance", ErrInvalidResponse)
	}
	return ports.BillingChargeResult{BalanceCents: dtoResp.BalanceCents}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: base url is required", ErrInvalidConfig)
	}
	base, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: base url", ErrInvalidConfig)
	}
	if base.Scheme != "http" && base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("%w: base url", ErrInvalidConfig)
	}
	base.Path = strings.TrimRight(base.Path, "/")
	return base, nil
}

func (c *Client) endpoint(path string) string {
	base := *c.baseURL
	base.Path = strings.TrimRight(base.Path, "/") + path
	return base.String()
}

func closeResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func validBillingChargeID(id string) bool {
	const prefix = "billchg_"
	if len(id) != len(prefix)+64 || !strings.HasPrefix(id, prefix) {
		return false
	}
	for _, ch := range id[len(prefix):] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func validateChargeRequest(request ports.BillingChargeRequest) error {
	if !validBillingChargeID(request.RequestID) || strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.Model) == "" {
		return fmt.Errorf("%w: required charge fields", ErrInvalidRequest)
	}
	if request.InputTokens < 0 || request.OutputTokens < 0 || request.AmountCents <= 0 || request.Currency != currencyRUB {
		return fmt.Errorf("%w: charge values", ErrInvalidRequest)
	}
	return nil
}

func readLimited(body io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: body too large", ErrInvalidResponse)
	}
	return data, nil
}

func decodeSingleJSON(body []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple json values")
	}
	return nil
}

type balanceResponse struct {
	Currency     string `json:"currency"`
	BalanceCents int64  `json:"balance_cents"`
}

type chargeRequest struct {
	RequestID    string `json:"request_id"`
	UserID       string `json:"user_id"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
}

type chargeResponse struct {
	BalanceCents *int64 `json:"balance_cents"`
}

var _ ports.BillingBalanceClient = (*Client)(nil)
var _ ports.BillingChargeClient = (*Client)(nil)
