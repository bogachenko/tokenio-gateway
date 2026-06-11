package forwarding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL          string
	APIKey           string
	HTTPClient       *http.Client
	MaxAttempts      int
	MaxDelay         time.Duration
	RateLimitEnabled bool
	RateLimitRPM     int
	RateLimitBurst   int
	RateLimitMaxWait time.Duration
}

type Client struct {
	baseURL          string
	apiKey           string
	httpClient       *http.Client
	maxAttempts      int
	maxDelay         time.Duration
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
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}
	var limiter *RateLimiter
	if cfg.RateLimitEnabled {
		limiter = NewRateLimiter(cfg.RateLimitRPM, cfg.RateLimitBurst, cfg.RateLimitMaxWait)
	}
	return &Client{
		baseURL:          strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:           strings.TrimSpace(cfg.APIKey),
		httpClient:       client,
		maxAttempts:      maxAttempts,
		maxDelay:         maxDelay,
		rateLimitMaxWait: cfg.RateLimitMaxWait,
		limiter:          limiter,
	}
}

func (c *Client) Forward(ctx context.Context, req ForwardRequest) (*Response, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "default"
	}
	req.Model = model

	budgetCtx := ctx
	cancel := func() {}
	var budgetDeadline time.Time
	if c.rateLimitMaxWait > 0 {
		budgetDeadline = time.Now().Add(c.rateLimitMaxWait)
		budgetCtx, cancel = context.WithTimeout(ctx, c.rateLimitMaxWait)
	}
	defer cancel()

	var lastErr error
	skipLimiterWait := false
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if c.limiter != nil && !skipLimiterWait {
			if err := c.limiter.Wait(budgetCtx, req.Model); err != nil {
				if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
					return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
				}
				return nil, err
			}
		}
		skipLimiterWait = false
		resp, retryAfter, err := c.doAttempt(budgetCtx, req.Method, req.Path, req.Header, req.Body)
		if err == nil {
			return resp, nil
		}
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
		}
		lastErr = err
		if !isRetryable(err) || attempt == c.maxAttempts {
			return nil, err
		}
		delay := retryAfter
		if delay > 0 {
			if !hasBudgetForDelay(budgetDeadline, delay) && ctx.Err() == nil {
				return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
			}
			skipLimiterWait = true
		} else {
			delay = exponentialBackoff(attempt, c.maxDelay)
			if delay > c.maxDelay {
				delay = c.maxDelay
			}
			if !hasBudgetForDelay(budgetDeadline, delay) && ctx.Err() == nil {
				return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
			}
		}
		if err := sleepContext(budgetCtx, delay); err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return nil, &RateLimitWaitTimeoutError{Model: req.Model, MaxWait: c.rateLimitMaxWait}
			}
			return nil, err
		}
	}
	return nil, lastErr
}

func hasBudgetForDelay(deadline time.Time, delay time.Duration) bool {
	if deadline.IsZero() {
		return true
	}
	return time.Until(deadline) >= delay
}

func (c *Client) doAttempt(ctx context.Context, method string, path string, header http.Header, body []byte) (*Response, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	copyForwardHeaders(req.Header, header)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
		return nil, retryAfter, &HTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return &Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: respBody}, 0, nil
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

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPError
	if ok := AsHTTPError(err, &httpErr); ok {
		return httpErr.StatusCode == http.StatusTooManyRequests || (httpErr.StatusCode >= 500 && httpErr.StatusCode <= 599)
	}
	return true
}

func AsHTTPError(err error, target **HTTPError) bool {
	if typed, ok := err.(*HTTPError); ok {
		*target = typed
		return true
	}
	return false
}

func exponentialBackoff(attempt int, maxDelay time.Duration) time.Duration {
	shift := attempt - 1
	if shift > 10 {
		shift = 10
	}
	delay := time.Duration(1<<shift) * 200 * time.Millisecond
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	parsed, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(parsed)
	if delay <= 0 {
		return 0
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
