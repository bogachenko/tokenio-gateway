package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	failure "github.com/bogachenko/tokenio-gateway/internal/ports/forwardingfailure"
)

var _ ports.ForwardingAdapter = (*Adapter)(nil)

type Config struct {
	Reseller domain.Reseller

	ResellerAPIKey string

	Transport http.RoundTripper

	MaxResponseBodyBytes int64
}

type Adapter struct {
	reseller             domain.Reseller
	resellerAPIKey       string
	transport            http.RoundTripper
	maxResponseBodyBytes int64
	baseURL              *url.URL
	classifier           ErrorClassifier
}

func NewAdapter(config Config, classifier ErrorClassifier) (*Adapter, error) {
	if strings.TrimSpace(config.Reseller.ID) == "" || strings.TrimSpace(string(config.Reseller.ProviderType)) == "" || strings.TrimSpace(config.Reseller.BaseURL) == "" || config.ResellerAPIKey == "" || config.Transport == nil || config.MaxResponseBodyBytes <= 0 || classifier == nil {
		return nil, fmt.Errorf("%w", ErrInvalidAdapterConfig)
	}
	baseURL, err := parseBaseURL(config.Reseller.BaseURL)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		reseller:             config.Reseller,
		resellerAPIKey:       config.ResellerAPIKey,
		transport:            config.Transport,
		maxResponseBodyBytes: config.MaxResponseBodyBytes,
		baseURL:              baseURL,
		classifier:           classifier,
	}, nil
}

func (a *Adapter) Forward(ctx context.Context, request ports.ForwardRequest) (ports.ForwardResponse, error) {
	if err := ctx.Err(); err != nil {
		kind := failure.FailureKindRequestError
		retry := false
		if errors.Is(err, context.DeadlineExceeded) {
			kind = failure.FailureKindTimeout
			retry = true
		}
		return ports.ForwardResponse{}, failure.NewFailure(
			kind,
			0,
			failure.AttemptStateNotSent,
			retry,
			err,
		)
	}
	if err := a.validateRouteAndRequest(request); err != nil {
		return ports.ForwardResponse{}, err
	}
	upstreamURL, err := buildUpstreamURL(a.baseURL, request.Path)
	if err != nil {
		return ports.ForwardResponse{}, err
	}
	body, err := a.prepareBody(request.Route, request.Body)
	if err != nil {
		return ports.ForwardResponse{}, err
	}

	var writeAttempted atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteHeaders: func() { writeAttempted.Store(true) },
		WroteRequest: func(httptrace.WroteRequestInfo) { writeAttempted.Store(true) },
	}
	ctx = httptrace.WithClientTrace(ctx, trace)
	req, err := http.NewRequestWithContext(ctx, request.Method, upstreamURL.String(), nil)
	if err != nil {
		return ports.ForwardResponse{}, fmt.Errorf("%w", ErrInvalidUpstreamURL)
	}
	req.Header = buildUpstreamHeaders(request.Headers, a.resellerAPIKey)
	req.Body = &attemptReadCloser{reader: bytes.NewReader(body), onRead: func() { writeAttempted.Store(true) }}
	req.ContentLength = int64(len(body))

	resp, err := a.transport.RoundTrip(req)
	if resp != nil {
		return a.handleResponse(resp, err)
	}
	if err != nil {
		if writeAttempted.Load() {
			return ports.ForwardResponse{}, failure.NewFailure(
				failure.FailureKindUncertainProcessing,
				0,
				failure.AttemptStateSentNoResponse,
				false,
				err,
			)
		}
		return ports.ForwardResponse{}, failure.NewFailure(
			forwardingTransportFailureKind(err),
			0,
			failure.AttemptStateNotSent,
			true,
			err,
		)
	}
	return ports.ForwardResponse{}, failure.NewFailure(
		failure.FailureKindMalformedResponse,
		0,
		failure.AttemptStateNotSent,
		true,
		nil,
	)
}

func (a *Adapter) handleResponse(resp *http.Response, cause error) (ports.ForwardResponse, error) {
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}
	defer resp.Body.Close()

	bodyBytes, truncated, readErr := readBounded(resp.Body, a.maxResponseBodyBytes)
	if readErr != nil {
		return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		if cause != nil {
			return ports.ForwardResponse{}, failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, cause)
		}
		if truncated {
			failure := failure.NewFailure(failure.FailureKindMalformedResponse, resp.StatusCode, failure.AttemptStateResponseReceived, false, ErrUpstreamResponseTooLarge)
			return ports.ForwardResponse{}, fmt.Errorf("%w: %w", ErrUpstreamResponseTooLarge, failure)
		}
		return ports.ForwardResponse{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: bodyBytes}, nil
	}
	classificationBody := bodyBytes
	if truncated && int64(len(classificationBody)) > a.maxResponseBodyBytes {
		classificationBody = classificationBody[:a.maxResponseBodyBytes]
	}
	classification := a.classifier.Classify(resp.StatusCode, cloneHeaders(resp.Header), classificationBody, truncated)
	return ports.ForwardResponse{}, failure.NewFailureWithRetryAfter(
		classification.Kind,
		resp.StatusCode,
		failure.AttemptStateResponseReceived,
		classification.RouteRetryCandidate,
		classification.RetryAfter,
		cause,
	)
}

func (a *Adapter) validateRouteAndRequest(request ports.ForwardRequest) error {
	if request.Method != http.MethodPost {
		return fmt.Errorf("%w", ErrInvalidForwardRequest)
	}
	route := request.Route
	if strings.TrimSpace(route.ID) == "" || strings.TrimSpace(route.ClientModel) == "" || route.APIFamily != domain.APIFamilyOpenAICompatible || route.ResellerID != a.reseller.ID || route.ProviderType != a.reseller.ProviderType {
		return fmt.Errorf("%w", ErrUnsupportedRoute)
	}
	expectedPath, ok := endpointPath(route.EndpointKind)
	if !ok {
		return fmt.Errorf("%w", ErrUnsupportedRoute)
	}
	parsed, err := url.ParseRequestURI(request.Path)
	if err != nil || parsed.Path != expectedPath {
		return fmt.Errorf("%w", ErrUnsupportedRoute)
	}
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		if route.ProviderModel != route.ClientModel {
			return fmt.Errorf("%w", ErrUnsupportedRoute)
		}
	case domain.ModelRewritePolicyProviderModel:
		// Body contract is validated later without mutating caller input.
	default:
		return fmt.Errorf("%w", ErrUnsupportedRoute)
	}
	return nil
}

func (a *Adapter) prepareBody(route domain.Route, body []byte) ([]byte, error) {
	switch route.ModelRewritePolicy {
	case domain.ModelRewritePolicyNone:
		return append([]byte(nil), body...), nil
	case domain.ModelRewritePolicyProviderModel:
		return rewriteTopLevelModel(body, route.ClientModel, route.ProviderModel)
	default:
		return nil, fmt.Errorf("%w", ErrUnsupportedRoute)
	}
}

func endpointPath(kind domain.EndpointKind) (string, bool) {
	switch kind {
	case domain.EndpointChat:
		return "/v1/chat/completions", true
	case domain.EndpointEmbeddings:
		return "/v1/embeddings", true
	case domain.EndpointImagesGeneration:
		return "/v1/images/generations", true
	default:
		return "", false
	}
}

func readBounded(r io.Reader, limit int64) ([]byte, bool, error) {
	limited := io.LimitReader(r, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return body, true, nil
	}
	return body, false, nil
}

type attemptReadCloser struct {
	reader *bytes.Reader
	onRead func()
}

func (r *attemptReadCloser) Read(p []byte) (int, error) {
	if r.onRead != nil {
		r.onRead()
	}
	return r.reader.Read(p)
}

func (r *attemptReadCloser) Close() error { return nil }

func forwardingTransportFailureKind(err error) failure.FailureKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return failure.FailureKindTimeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return failure.FailureKindTimeout
	}
	return failure.FailureKindConnectionError
}
