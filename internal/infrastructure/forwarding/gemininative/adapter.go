package gemininative

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidAdapterConfig     = errors.New("invalid gemini native adapter config")
	ErrInvalidForwardRequest    = errors.New("invalid gemini native forward request")
	ErrUnsupportedRoute         = errors.New("unsupported gemini native route")
	ErrInvalidUpstreamURL       = errors.New("invalid gemini native upstream URL")
	ErrUpstreamResponseTooLarge = errors.New("gemini native upstream response too large")
)

type Config struct { Reseller domain.Reseller; ResellerAPIKey string; Transport http.RoundTripper; MaxResponseBodyBytes int64 }

type Adapter struct { reseller domain.Reseller; resellerAPIKey string; transport http.RoundTripper; maxResponseBodyBytes int64; baseURL *url.URL }

var _ ports.ForwardingAdapter = (*Adapter)(nil)

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.Reseller.ID) == "" || strings.TrimSpace(string(config.Reseller.ProviderType)) == "" || strings.TrimSpace(config.Reseller.BaseURL) == "" || config.ResellerAPIKey == "" || config.Transport == nil || config.MaxResponseBodyBytes <= 0 { return nil, ErrInvalidAdapterConfig }
	baseURL, err := parseBaseURL(config.Reseller.BaseURL); if err != nil { return nil, err }
	return &Adapter{reseller: config.Reseller, resellerAPIKey: config.ResellerAPIKey, transport: config.Transport, maxResponseBodyBytes: config.MaxResponseBodyBytes, baseURL: baseURL}, nil
}

func (a *Adapter) Forward(ctx context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	if a == nil || a.transport == nil || a.baseURL == nil || a.resellerAPIKey == "" || a.maxResponseBodyBytes <= 0 { return ports.ForwardResponse{}, ErrInvalidAdapterConfig }
	if ctx == nil { return ports.ForwardResponse{}, ErrInvalidForwardRequest }
	if err := ctx.Err(); err != nil { return ports.ForwardResponse{}, err }
	if err := a.validateRouteAndRequest(request); err != nil { return ports.ForwardResponse{}, err }
	path, err := preparePath(request.Route, request.Path); if err != nil { return ports.ForwardResponse{}, err }
	upstreamURL, err := buildUpstreamURL(a.baseURL, path); if err != nil { return ports.ForwardResponse{}, err }
	body := append([]byte(nil), request.Body...)
	req, err := http.NewRequestWithContext(ctx, request.Method, upstreamURL.String(), bytes.NewReader(body)); if err != nil { return ports.ForwardResponse{}, ErrInvalidUpstreamURL }
	req.Header = buildUpstreamHeaders(request.Headers, a.resellerAPIKey)
	req.ContentLength = int64(len(body))
	resp, err := a.transport.RoundTrip(req); if err != nil { return ports.ForwardResponse{}, err }
	if resp == nil { return ports.ForForwardResponse{}, ErrInvalidForwardRequest }
	return handleResponse(resp, a.maxResponseBodyBytes)
}

func (a *Adapter) validateRouteAndRequest(request ports.ForwardRequest) error {
	if request.Method != http.MethodPost { return ErrInvalidForwardRequest }
	route := request.Route
	if strings.TrimSpace(route.ID) == "" || strings.TrimSpace(route.ClientModel) == "" || route.APIFamily != domain.APIFamilyGeminiNative || route.ResellerID != a.reseller.ID || route.ProviderType != a.reseller.ProviderType { return ErrUnsupportedRoute }
	switch route.EndpointKind { case domain.EndpointChat, domain.EndpointEmbeddings: default: return ErrUnsupportedRoute }
	parsed, err := url.ParseRequestURI(request.Path); if err != nil || !strings.HasPrefix(parsed.Path, "/v1beta/models/") || parsed.RawQuery != "" || parsed.Fragment != "" { return ErrUnsupportedRoute }
	model, operation, ok := geminiPathModelAndOperation(parsed.Path)
	if !ok || model != route.ClientModel || !operationMatchesEndpoint(operation, route.EndpointKind) { return ErrUnsupportedRoute }
	switch route.ModelRewritePolicy { case domain.ModelRewritePolicyNone: if route.ProviderModel != route.ClientModel { return ErrUnsupportedRoute }; case domain.ModelRewritePolicyProviderModel: if strings.TrimSpace(route.ProviderModel) == "" { return ErrUnsupportedRoute }; default: return ErrUnsupportedRoute }
	return nil
}

func preparePath(route domain.Route, path string) (string, error) {
	switch route.ModelRewritePolicy { case domain.ModelRewritePolicyNone: return path, nil; case domain.ModelRewritePolicyProviderModel:
		parsed, err := url.ParseRequestURI(path); if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" { return "", ErrUnsupportedRoute }
		model, operation, ok := geminiPathModelAndOperation(parsed.Path); if !ok || model != route.ClientModel { return "", ErrUnsupportedRoute }
		return "/v1beta/models/" + url.PathEscape(route.ProviderModel) + ":" + operation, nil
	default: return "", ErrUnsupportedRoute }
}

func geminiPathModelAndOperation(path string) (string, string, bool) {
	tail := strings.TrimPrefix(path, "/v1beta/models/"); if tail == path || tail == "" || strings.Contains(tail, "/") { return "", "", false }
	model, operation, ok := strings.Cut(tail, ":"); if !ok || model == "" || operation == "" { return "", "", false }
	unescaped, err := url.PathUnescape(model); if err != nil || strings.TrimSpace(unescaped) == "" || strings.Contains(unescaped, "/") { return "", "", false }
	return unescaped, operation, true
}

func operationMatchesEndpoint(operation string, endpoint domain.EndpointKind) bool {
	switch endpoint { case domain.EndpointChat: return operation == "generateContent" || operation == "streamGenerateContent"; case domain.EndpointEmbeddings: return operation == "embedContent" || operation == "batchEmbedContents"; default: return false }
}

func handleResponse(resp *http.Response, limit int64) (ports.ForwardResponse, error) {
	if resp.Body == nil { resp.Body = io.NopCloser(bytes.NewReader(nil)) }
	defer resp.Body.Close()
	body, truncated, err := readBounded(resp.Body, limit)
	if err != nil { return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, err) }
	if truncated { return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, ErrUpstreamResponseTooLarge) }
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		usage, usageErr := ExtractUsage(body)
		if usageErr != nil && !errors.Is(usageErr, ErrUsageNotFound) { return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, usageErr) }
		response := ports.ForwardResponse{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}
		if usageErr == nil { response.Usage = &ports.ForwardUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens} }
		return response, nil
	}
	return ports.ForwardResponse{}, classifyFailure(resp.StatusCode, cloneHeaders(resp.Header), body)
}

func classifyFailure(status int, headers map[string][]string, body []byte) error { return forwarding.NewFailure(forwarding.FailureKindUpstreamError, status, forwarding.AttemptStateResponseReceived, false, errors.New(string(body))) }

func retryAfter(headers map[string][]string, now time.Time) (bool, time.Duration, time.Time) { values := headers["Retry-After"]; if len(values) == 0 { return false, 0, time.Time{} }; seconds, err := strconv.Atoi(values[0]); if err != nil { return true, 0, time.Time{} }; return true, time.Duration(seconds) * time.Second, now.Add(time.Duration(seconds) * time.Second) }
