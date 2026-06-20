package ollamanative

import (
	"bytes"
	"context"
	"encoding/json"
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
	ErrInvalidAdapterConfig     = errors.New("invalid ollama native adapter config")
	ErrInvalidForwardRequest    = errors.New("invalid ollama native forward request")
	ErrUnsupportedRoute         = errors.New("unsupported ollama native route")
	ErrInvalidUpstreamURL       = errors.New("invalid ollama native upstream URL")
	ErrUpstreamResponseTooLarge = errors.New("ollama native upstream response too large")
	ErrInvalidUsage             = errors.New("invalid ollama native usage")
	ErrUsageNotFound            = errors.New("ollama native usage metadata not found")
)

type Config struct {
	Reseller             domain.Reseller
	ResellerAPIKey       string
	Transport            http.RoundTripper
	MaxResponseBodyBytes int64
}

type Adapter struct {
	reseller             domain.Reseller
	resellerAPIKey       string
	transport            http.RoundTripper
	maxResponseBodyBytes int64
	baseURL              *url.URL
}

var _ ports.ForwardingAdapter = (*Adapter)(nil)

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.Reseller.ID) == "" || strings.TrimSpace(string(config.Reseller.ProviderType)) == "" || strings.TrimSpace(config.Reseller.BaseURL) == "" || config.ResellerAPIKey == "" || config.Transport == nil || config.MaxResponseBodyBytes <= 0 {
		return nil, ErrInvalidAdapterConfig
	}
	baseURL, err := parseBaseURL(config.Reseller.BaseURL)
	if err != nil {
		return nil, err
	}
	return &Adapter{reseller: config.Reseller, resellerAPIKey: config.ResellerAPIKey, transport: config.Transport, maxResponseBodyBytes: config.MaxResponseBodyBytes, baseURL: baseURL}, nil
}

func (a *Adapter) Forward(ctx context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	if a == nil || a.transport == nil || a.baseURL == nil || a.resellerAPIKey == "" || a.maxResponseBodyBytes <= 0 {
		return ports.ForwardResponse{}, ErrInvalidAdapterConfig
	}
	if ctx == nil {
		return ports.ForwardResponse{}, ErrInvalidForwardRequest
	}
	if err := ctx.Err(); err != nil {
		return ports.ForwardResponse{}, err
	}
	if err := a.validateRouteAndRequest(request); err != nil {
		return ports.ForwardResponse{}, err
	}
	body, err := prepareBody(request.Route, request.Body)
	if err != nil {
		return ports.ForwardResponse{}, err
	}
	upstreamURL, err := buildUpstreamURL(a.baseURL, request.Path)
	if err != nil {
		return ports.ForwardResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		return ports.ForwardResponse{}, ErrInvalidUpstreamURL
	}
	req.Header = buildUpstreamHeaders(request.Headers, a.resellerAPIKey)
	req.ContentLength = int64(len(body))

	resp, err := a.transport.RoundTrip(req)
	if err != nil {
		return ports.ForwardResponse{}, err
	}
	if resp == nil {
		return ports.ForwardResponse{}, ErrInvalidForwardRequest
	}
	return handleResponse(resp, a.maxResponseBodyBytes)
}

func (a *Adapter) validateRouteAndRequest(request ports.ForwardRequest) error {
	if request.Method != http.MethodPost {
		return ErrInvalidForwardRequest
	}
	route := request.Route
	if strings.TrimSpace(route.ID) == "" || strings.TrimSpace(route.ClientModel) == "" || route.APIFamily != domain.APIFamilyOllamaNative || route.ResellerID != a.reseller.ID || route.ProviderType != a.reseller.ProviderType || route.ProviderType != domain.ProviderOllama {
		return ErrUnsupportedRoute
	}
	switch route.EndpointKind {
	case domain.EndpointChat:
		if request.Path != "/api/chat" && request.Path != "/api/generate" {
			return ErrUnsupportedRoute
		}
	case domain.EndpointEmbeddings:
		if request.Path != "/api/embeddings" {
			return ErrUnsupportedRoute
		}
	default:
		return ErrUnsupportedRoute
	}
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		if route.ProviderModel != route.ClientModel {
			return ErrUnsupportedRoute
		}
	case domain.ModelRewritePolicyProviderModel:
		if strings.TrimSpace(route.ProviderModel) == "" {
			return ErrUnsupportedRoute
		}
	default:
		return ErrUnsupportedRoute
	}
	return nil
}

func prepareBody(route domain.Route, body []byte) ([]byte, error) {
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return append([]byte(nil), body...), nil
	case domain.ModelRewritePolicyProviderModel:
		return replaceTopModelBytes(body, route.ClientModel, route.ProviderModel)
	default:
		return nil, ErrUnsupportedRoute
	}
}

func ExtractUsage(body []byte) (Usage, error) {
	var payload map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, ErrUsageNotFound
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Usage{}, ErrInvalidUsage
	}
	inputTokens, inputPresent, inputErr := ollamaTokenCount(payload, "prompt_eval_count")
	if inputErr != nil {
		return Usage{}, inputErr
	}
	outputTokens, outputPresent, outputErr := ollamaTokenCount(payload, "eval_count")
	if outputErr != nil {
		return Usage{}, outputErr
	}
	if !inputPresent && !outputPresent {
		return Usage{}, ErrUsageNotFound
	}
	if !inputPresent || !outputPresent {
		return Usage{}, ErrInvalidUsage
	}
	return Usage{InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

func optionalNonNegativeInt64(payload map[string]json.RawMessage, field string) (int64, bool, error) {
	raw, ok := payload[field]
	if !ok || string(raw) == "null" {
		return 0, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return 0, false, ErrInvalidForwardRequest
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return 0, false, ErrInvalidForwardRequest
	}
	value, err := number.Int64()
	if err != nil || value < 0 || number.String() != strconv.FormatInt(value, 10) {
		return 0, false, ErrInvalidForwardRequest
	}
	return value, true, nil
}

func handleResponse(resp *http.Response, limit int64) (ports.ForwardResponse, error) {
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}
	defer resp.Body.Close()
	body, truncated, err := readBounded(resp.Body, limit)
	if err != nil {
		return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, err)
	}
	if truncated {
		return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, ErrUpstreamResponseTooLarge)
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		usage, usageErr := ExtractUsage(body)
		if usageErr != nil && !errors.Is(usageErr, ErrUsageNotFound) {
			return ports.ForwardResponse{}, forwarding.NewFailure(forwarding.FailureKindMalformedResponse, resp.StatusCode, forwarding.AttemptStateResponseReceived, false, usageErr)
		}
		response := ports.ForwardResponse{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}
		if usageErr == nil {
			response.Usage = &ports.ForwardUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
		}
		return response, nil
	}
	classification := classifyFailure(resp.StatusCode, resp.Header, body)
	return ports.ForwardResponse{}, forwarding.NewFailureWithRetryAfter(classification.Kind, resp.StatusCode, forwarding.AttemptStateResponseReceived, classification.RouteRetryCandidate, classification.RetryAfter, nil)
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidAdapterConfig
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, ErrInvalidAdapterConfig
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func buildUpstreamURL(base *url.URL, path string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(path)
	if err != nil || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidUpstreamURL
	}
	if strings.HasPrefix(path, "//") || strings.Contains(parsed.Path, " ") {
		return nil, ErrInvalidUpstreamURL
	}
	result := *base
	basePath := strings.TrimRight(result.Path, "/")
	result.Path = basePath + parsed.Path
	result.RawQuery = ""
	result.Fragment = ""
	return &result, nil
}

func buildUpstreamHeaders(input map[string][]string, resellerAPIKey string) http.Header {
	result := http.Header{}
	connectionTokens := map[string]struct{}{}
	for name, values := range input {
		if strings.EqualFold(name, "Connection") {
			for _, value := range values {
				for _, token := range strings.Split(value, ",") {
					connectionTokens[strings.ToLower(strings.TrimSpace(token))] = struct{}{}
				}
			}
		}
	}
	for name, values := range input {
		if shouldStripHeader(name, connectionTokens) {
			continue
		}
		for _, value := range values {
			result.Add(name, value)
		}
	}
	if result.Get("Content-Type") == "" {
		result.Set("Content-Type", "application/json")
	}
	result.Set("Authorization", "Bearer "+resellerAPIKey)
	return result
}

func shouldStripHeader(name string, connectionTokens map[string]struct{}) bool {
	lower := strings.ToLower(name)
	if _, ok := connectionTokens[lower]; ok {
		return true
	}
	switch lower {
	case "authorization", "proxy-authorization", "x-"+"api-"+"key", "x-goog-"+"api-"+"key", "x-service-"+"token", "x-local-request-id", "x-billing-"+"token", "x-wallet-id", "connection", "transfer-encoding", "te", "trailer", "upgrade", "content-length", "host":
		return true
	default:
		return false
	}
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return body, true, nil
	}
	return body, false, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func ollamaTokenCount(payload map[string]json.RawMessage, key string) (int64, bool, error) {
	raw, ok := payload[key]
	if !ok {
		return 0, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return 0, true, ErrInvalidUsage
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return 0, true, ErrInvalidUsage
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value < 0 {
		return 0, true, ErrInvalidUsage
	}
	return value, true, nil
}

func classifyFailure(statusCode int, headers http.Header, body []byte) forwarding.Classification {
	bodyText := strings.ToLower(string(body))
	switch {
	case statusCode == http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(headers.Get("Retry-After"))
		return forwarding.Classification{Kind: forwarding.FailureKindRateLimited, RouteRetryCandidate: true, RetryAfter: retryAfter}
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return forwarding.Classification{Kind: forwarding.FailureKindAuthError}
	case statusCode == http.StatusPaymentRequired || strings.Contains(bodyText, "quota") || strings.Contains(bodyText, "resource"):
		return forwarding.Classification{Kind: forwarding.FailureKindQuotaExceeded}
	case statusCode >= 500:
		return forwarding.Classification{Kind: forwarding.FailureKindProvider5XX, RouteRetryCandidate: true}
	default:
		return forwarding.Classification{Kind: forwarding.FailureKindRequestError}
	}
}

func parseRetryAfter(value string) forwarding.RetryAfter {
	value = strings.TrimSpace(value)
	if value == "" {
		return forwarding.RetryAfter{}
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return forwarding.RetryAfter{}
	}
	retryAfter, err := forwarding.NewRetryAfterDelay(time.Duration(seconds) * time.Second)
	if err != nil {
		return forwarding.RetryAfter{}
	}
	return retryAfter
}
