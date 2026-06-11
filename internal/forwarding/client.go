package forwarding

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	// Deprecated: retry policy belongs to the route executor, not to the
	// one-attempt forwarding adapter. Kept temporarily so old struct literals
	// do not fail while the runtime is being split into layers.
	MaxAttempts int

	// Deprecated: retry/backoff belongs to the route executor.
	MaxDelay time.Duration

	RateLimitEnabled bool
	RateLimitRPM     int
	RateLimitBurst   int
	RateLimitMaxWait time.Duration
}

type Client struct {
	baseURL          string
	apiKey           string
	httpClient       *http.Client
	rateLimitMaxWait time.Duration
	limiter          *RateLimiter
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type HTTPError struct {
	StatusCode int
	Body       string
}

type ForwardRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
	Model  string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("upstream error: status=%d body=%s", e.StatusCode, e.Body)
}

func NewClient(cfg Config) *Client {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	var limiter *RateLimiter
	if cfg.RateLimitEnabled {
		limiter = NewRateLimiter(cfg.RateLimitRPM, cfg.RateLimitBurst, cfg.RateLimitMaxWait)
	}

	return &Client{
		baseURL:          strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:           strings.TrimSpace(cfg.APIKey),
		httpClient:       client,
		rateLimitMaxWait: cfg.RateLimitMaxWait,
		limiter:          limiter,
	}
}

// Forward performs exactly one upstream HTTP attempt.
//
// It deliberately does not retry, sleep, back off, or interpret Retry-After.
// Retry/fallback decisions require route metadata, API-family compatibility,
// ledger/reseller reserve state, and unsafe-processing boundaries; those
// decisions belong to the route executor.
//
// Any HTTP response received from upstream is returned as Response, including
// 4xx, 429, and 5xx. Transport/build/read failures are returned as errors.
func (c *Client) Forward(ctx context.Context, req ForwardRequest) (*Response, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "default"
	}
	req.Model = model

	budgetCtx := ctx
	cancel := func() {}
	if c.rateLimitMaxWait > 0 {
		budgetCtx, cancel = context.WithTimeout(ctx, c.rateLimitMaxWait)
	}
	defer cancel()

	if c.limiter != nil {
		if err := c.limiter.Wait(budgetCtx, req.Model); err != nil {
			if budgetCtx.Err() != nil && ctx.Err() == nil {
				return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
			}
			return nil, err
		}
	}

	return c.doRequest(ctx, req.Method, req.Path, req.Header, req.Body)
}

func (c *Client) doRequest(ctx context.Context, method string, path string, header http.Header, body []byte) (*Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	copyForwardHeaders(request.Header, header)
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return &Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       responseBody,
	}, nil
}

func copyForwardHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		switch lower {
		case "authorization", "host", "connection", "content-length", "transfer-encoding", "upgrade", "proxy-authenticate", "proxy-authorization", "te", "trailer":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func AsHTTPError(err error, target **HTTPError) bool {
	if typed, ok := err.(*HTTPError); ok {
		*target = typed
		return true
	}
	return false
}
