package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Config struct {
	BaseURL      string
	ServiceToken string
	HTTPClient   *http.Client
}

type Client struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(cfg Config) *Client {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		serviceToken: strings.TrimSpace(cfg.ServiceToken),
		httpClient:   client,
	}
}

type BalanceResponse struct {
	Currency     string `json:"currency"`
	BalanceCents int64  `json:"balance_cents"`
}

type ChargeRequest struct {
	RequestID    string `json:"request_id"`
	UserID       string `json:"user_id"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
}

type ChargeResponse struct {
	RequestID    string `json:"request_id"`
	UserID       string `json:"user_id"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	BalanceCents int64  `json:"balance_cents"`
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("billing error: status=%d body=%s", e.StatusCode, e.Body)
}

func (c *Client) Balance(ctx context.Context, userJWT string) (BalanceResponse, error) {
	var out BalanceResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/wallet/balance", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+userJWT)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decode billing balance: %w", err)
	}
	return out, nil
}

func (c *Client) Charge(ctx context.Context, charge ChargeRequest) (ChargeResponse, error) {
	var out ChargeResponse
	body, err := json.Marshal(charge)
	if err != nil {
		return out, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/usage/charge", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Service-Token", c.serviceToken)
	req.Header.Set("Idempotency-Key", charge.RequestID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return out, &HTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil {
			return out, fmt.Errorf("decode billing charge: %w", err)
		}
	}
	return out, nil
}
