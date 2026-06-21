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

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/transportfailure"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	failure "github.com/bogachenko/tokenio-gateway/internal/ports/forwardingfailure"
)

var (
	ErrInvalidAdapterConfig     = errors.New("invalid gemini native adapter config")
	ErrInvalidForwardRequest    = errors.New("invalid gemini native forward request")
	ErrUnsupportedRoute         = errors.New("unsupported gemini native route")
	ErrInvalidUpstreamURL       = errors.New("invalid gemini native upstream URL")
	ErrUpstreamResponseTooLarge = errors.New("gemini native upstream response too large")
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
	if err != nil { return nil, err }
	return &Adapter{reseller: config.Reseller, resellerAPIKey: config.ResellerAPIKey, transport: config.Transport, maxResponseBodyBytes: config.MaxResponseBodyBytes, baseURL: baseURL}, nil
}

func (a *Adapter) Forward(ctx context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	if a == nil || a.transport == nil || a.baseURL == nil || a.resellerAPIKey == "" || a.maxResponseBodyBytes <= 0 { return ports.ForwardResponse{}, ErrInvalidAdapterConfig }
	if ctx == nil { return ports.ForwardResponse{}, ErrInvalidForwardRequest }
	if err := ctx.Err(); err != nil { return transportfailure.ContextErr(err) }
	if err := a.validateRouteAndRequest(request); err != nil { return ports.ForwardResponse{}, err }
	path, err := preparePath(request.Route, request.Path)
	if err != nil { return ports.ForwardResponse{}, err }
	upstreamURL, err := buildUpstreamURL(a.baseURL, path)
	if err != nil { return ports.ForwardResponse{}, err }
	body := append([]byte(nil), request.Body...)
	tracker := &transportfailure.WriteTracker{}
	ctx = transportfailure.WithTrace(ctx, tracker)
	req, err := http.NewRequestWithContext(ctx, request.Method, upstreamURL.String(), nil)
	if err != nil { return ports.ForwardResponse{}, ErrInvalidUpstreamURL }
	req.Header = buildUpstreamHeaders(request.Headers, a.resellerAPIKey)
	req.Body = transportfailure.NewTrackedBody(body, tracker)
	req.ContentLength = int64(len(body))
	resp, err := a.transport.RoundTrip(req)
	if resp != nil { return handleResponse(resp, a.maxResponseBodyBytes, err) }
	if err != nil { return transportfailure.TransportErr(err, tracker.Attempted()) }
	return transportfailure.NilResponse()
}

func (a *Adapter) validateRouteAndRequest(request ports.ForwardRequest) error {
	if request.Method != http.MethodPost { return ErrInvalidForwardRequest }
	route := request.Route
	if strings.TrimSpace(route.ID) == "" || strings.TrimSpace(route.ClientModel) == "" || route.APIFamily != domain.APIFamilyGeminiNative || route.ResellerID != a.reseller.ID || route.ProviderType != a.reseller.ProviderType { return ErrUnsupportedRoute }
	switch route.EndpointKind { case domain.EndpointChat, domain.EndpointEmbeddings: default: return ErrUnsupportedRoute }
	parsed, err := url.ParseRequestURI(request.Path)
	if err != nil || !strings.HasPrefix(parsed.Path, "/v1beta/models/") || parsed.Fragment != "" { return ErrUnsupportedRoute }
	model, operation, ok := geminiPathModelAndOperation(parsed.Path)
	if !ok || model != route.ClientModel || !operationMatchesEndpoint(operation, route.EndpointKind) { return ErrUnsupportedRoute }
	switch route.ModelRewritePolicy { case domain.ModelRewritePolicyNone: if route.ProviderModel != route.ClientModel { return ErrUnsupportedRoute }; case domain.ModelRewritePolicyProviderModel: if strings.TrimSpace(route.ProviderModel) == "" { return ErrUnsupportedRoute }; default: return ErrUnsupportedRoute }
	return nil
}

func preparePath(route domain.Route, path string) (string, error) {
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return path, nil
	case domain.ModelRewritePolicyProviderModel:
		parsed, err := url.ParseRequestURI(path)
		if err != nil || parsed.Fragment != "" { return "", ErrUnsupportedRoute }
		model, operation, ok := geminiPathModelAndOperation(parsed.Path)
		if !ok || model != route.ClientModel { return "", ErrUnsupportedRoute }
		rewritten := "/v1beta/models/" + url.PathEscape(route.ProviderModel) + ":" + operation
		if parsed.RawQuery != "" { rewritten += "?" + parsed.RawQuery }
		return rewritten, nil
	default:
		return "", ErrUnsupportedRoute
	}
}

func geminiPathModelAndOperation(path string) (string, string, bool) {
	tail := strings.TrimPrefix(path, "/v1beta/models/")
	if tail == path || tail == "" || strings.Contains(tail, "/") { return "", "", false }
	model, operation, ok := strings.Cut(tail, ":")
	if !ok || model == "" || operation == "" { return "", "", false }
	unescaped, err := url.PathUnescape(model)
	if err != nil || strings.TrimSpace(unescaped) == "" || strings.Contains(unescaped, "/") { return "", "", false }
	return unescaped, operation, true
}

func operationMatchesEndpoint(operation string, endpoint domain.EndpointKind) bool {
	switch endpoint {
	case domain.EndpointChat:
		return operation == "generateContent" || operation == "streamGenerateContent"
	case domain.EndpointEmbeddings:
		return operation == "embedContent" || operation == "batchEmbedContents"
	default:
		return false
	}
}

func handleResponse(resp *http.Response, limit int64, cause error) (ports.ForwardResponse, error) {
	if resp.Body == nil { resp.Body = io.NopCloser(bytes.NewReader(nil)) }
	defer resp.Body.Close()
	body, truncated, err := readBounded(resp.Body, limit)
	if err != nil { return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, err) }
	if truncated { return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, ErrUpstreamResponseTooLarge) }
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		if cause != nil { return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, cause) }
		usage, usageErr := ExtractUsage(body)
		if usageErr != nil && !errors.Is(usageErr, ErrUsageNotFound) { return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, usageErr) }
		response := ports.ForwardResponse{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}
		if usageErr == nil { response.Usage = &ports.ForwardUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens} }
		return response, nil
	}
	classification := classifyFailure(resp.StatusCode, resp.Header, body)
	return ports.ForwardResponse{}, failure.NewFailureWithRetryAfter(classification.Kind, resp.StatusCode, failure.AttemptStateResponseReceived, classification.RouteRetryCandidate, classification.RetryAfter, cause)
}

func classifyFailure(statusCode int, headers http.Header, body []byte) failure.Classification {
	switch {
	case statusCode == http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(headers.Get("Retry-After")); return failure.Classification{Kind: failure.FailureKindRateLimited, RouteRetryCandidate: true, RetryAfter: retryAfter}
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return failure.Classification{Kind: failure.FailureKindAuthError}
	case statusCode == http.StatusPaymentRequired || strings.Contains(strings.ToLower(string(body)), "quota"):
		return failure.Classification{Kind: failure.FailureKindQuotaExceeded}
	case statusCode >= 500:
		return failure.Classification{Kind: failure.FailureKindProvider5XX, RouteRetryCandidate: true}
	default:
		return failure.Classification{Kind: failure.FailureKindRequestError}
	}
}

func parseRetryAfter(value string) failure.RetryAfter {
	value = strings.TrimSpace(value)
	if value == "" { return failure.RetryAfter{} }
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 { return failure.RetryAfter{} }
	retryAfter, err := failure.NewRetryAfterDelay(time.Duration(seconds) * time.Second)
	if err != nil { return failure.RetryAfter{} }
	return retryAfter
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" { return nil, ErrInvalidAdapterConfig }
	if parsed.Scheme != "https" && parsed.Scheme != "http" { return nil, ErrInvalidAdapterConfig }
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func buildUpstreamURL(base *url.URL, path string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(path)
	if err != nil || parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || parsed.Fragment != "" { return nil, ErrInvalidUpstreamURL }
	if strings.HasPrefix(path, "//") || strings.Contains(parsed.Path, " ") { return nil, ErrInvalidUpstreamURL }
	result := *base
	basePath := strings.TrimRight(result.Path, "/")
	result.Path = basePath + parsed.Path
	result.RawQuery = parsed.RawQuery
	result.Fragment = ""
	return &result, nil
}

func buildUpstreamHeaders(input map[string][]string, resellerAPIKey string) http.Header {
	result := http.Header{}
	connectionTokens := map[string]struct{}{}
	for name, values := range input { if strings.EqualFold(name, "Connection") { for _, value := range values { for _, token := range strings.Split(value, ",") { connectionTokens[strings.ToLower(strings.TrimSpace(token))] = struct{}{} } } } }
	for name, values := range input { if shouldStripHeader(name, connectionTokens) { continue }; for _, value := range values { result.Add(name, value) } }
	if result.Get("Content-Type") == "" { result.Set("Content-Type", "application/json") }
	result.Set("x-goog-api-key", resellerAPIKey)
	return result
}

func shouldStripHeader(name string, connectionTokens map[string]struct{}) bool {
	lower := strings.ToLower(name)
	if _, ok := connectionTokens[lower]; ok { return true }
	switch lower { case "authorization", "proxy-authorization", "x-api-key", "x-goog-api-key", "x-service-token", "x-local-request-id", "x-billing-token", "x-wallet-id", "connection", "transfer-encoding", "te", "trailer", "upgrade", "content-length", "host": return true; default: return false }
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil { return nil, false, err }
	if int64(len(body)) > limit { return body, true, nil }
	return body, false, nil
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers { result[key] = append([]string(nil), values...) }
	return result
}
