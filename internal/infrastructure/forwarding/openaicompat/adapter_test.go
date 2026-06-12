package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"reflect"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	resellerSecret = "rk_reseller_secret"
	clientSecret   = "sk_client_secret"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type spyClassifier struct {
	calls     int
	status    int
	headers   map[string][]string
	body      []byte
	truncated bool
	result    forwarding.Classification
}

func (s *spyClassifier) Classify(statusCode int, headers map[string][]string, body []byte, bodyTruncated bool) forwarding.Classification {
	s.calls++
	s.status = statusCode
	s.headers = headers
	s.body = append([]byte(nil), body...)
	s.truncated = bodyTruncated
	if s.result.Kind != "" {
		return s.result
	}
	return StatusClassifier{}.Classify(statusCode, headers, body, bodyTruncated)
}

type closeTrackingBody struct {
	*bytes.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error { b.closed = true; return nil }

func baseReseller() domain.Reseller {
	return domain.Reseller{ID: "reseller-1", ProviderType: domain.ProviderOpenAI, BaseURL: "https://provider.example/api"}
}

func baseRoute(kind domain.EndpointKind) domain.Route {
	path, _ := endpointPath(kind)
	_ = path
	return domain.Route{ID: "route-1", ResellerID: "reseller-1", ProviderType: domain.ProviderOpenAI, APIFamily: domain.APIFamilyOpenAICompatible, EndpointKind: kind, ClientModel: "client-model", ProviderModel: "client-model", ModelRewritePolicy: domain.ModelRewritePolicyNone}
}

func newTestAdapter(t *testing.T, rt http.RoundTripper, classifier ErrorClassifier, opts ...func(*Config)) *Adapter {
	t.Helper()
	cfg := Config{Reseller: baseReseller(), ResellerAPIKey: resellerSecret, Transport: rt, MaxResponseBodyBytes: 64}
	for _, opt := range opts {
		opt(&cfg)
	}
	adapter, err := NewAdapter(cfg, classifier)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return adapter
}

func forwardReq(kind domain.EndpointKind) ports.ForwardRequest {
	path, _ := endpointPath(kind)
	return ports.ForwardRequest{Route: baseRoute(kind), Method: http.MethodPost, Path: path, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"model":"client-model"}`)}
}

func TestNewAdapterConfigValidation(t *testing.T) {
	valid := Config{Reseller: baseReseller(), ResellerAPIKey: resellerSecret, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }), MaxResponseBodyBytes: 1}
	tests := []struct {
		name       string
		mutate     func(*Config)
		classifier ErrorClassifier
	}{
		{"empty reseller ID", func(c *Config) { c.Reseller.ID = "" }, StatusClassifier{}},
		{"empty provider type", func(c *Config) { c.Reseller.ProviderType = "" }, StatusClassifier{}},
		{"empty BaseURL", func(c *Config) { c.Reseller.BaseURL = "" }, StatusClassifier{}},
		{"empty reseller API key", func(c *Config) { c.ResellerAPIKey = "" }, StatusClassifier{}},
		{"nil RoundTripper", func(c *Config) { c.Transport = nil }, StatusClassifier{}},
		{"non-positive response limit", func(c *Config) { c.MaxResponseBodyBytes = 0 }, StatusClassifier{}},
		{"nil classifier", func(c *Config) {}, nil},
		{"relative BaseURL", func(c *Config) { c.Reseller.BaseURL = "provider.example" }, StatusClassifier{}},
		{"unsupported scheme", func(c *Config) { c.Reseller.BaseURL = "ftp://provider.example" }, StatusClassifier{}},
		{"BaseURL without host", func(c *Config) { c.Reseller.BaseURL = "https:///path" }, StatusClassifier{}},
		{"BaseURL with userinfo", func(c *Config) { c.Reseller.BaseURL = "https://user:pass@provider.example" }, StatusClassifier{}},
		{"BaseURL with query", func(c *Config) { c.Reseller.BaseURL = "https://provider.example?key=secret" }, StatusClassifier{}},
		{"BaseURL with fragment", func(c *Config) { c.Reseller.BaseURL = "https://provider.example/#fragment" }, StatusClassifier{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			_, err := NewAdapter(cfg, tt.classifier)
			if !errors.Is(err, ErrInvalidAdapterConfig) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestRouteValidation(t *testing.T) {
	accepted := []domain.EndpointKind{domain.EndpointChat, domain.EndpointEmbeddings, domain.EndpointImagesGeneration}
	for _, kind := range accepted {
		t.Run(string(kind)+" accepted", func(t *testing.T) {
			adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, "ok"), nil }), StatusClassifier{})
			_, err := adapter.Forward(t.Context(), forwardReq(kind))
			if err != nil {
				t.Fatalf("Forward: %v", err)
			}
		})
	}
	modelPath := "/v1/" + "models"
	cases := []struct {
		name   string
		mutate func(*ports.ForwardRequest)
		want   error
	}{
		{"other API family", func(r *ports.ForwardRequest) { r.Route.APIFamily = domain.APIFamilyGeminiNative }, ErrUnsupportedRoute},
		{"wrong endpoint kind path", func(r *ports.ForwardRequest) { r.Route.EndpointKind = domain.EndpointEmbeddings }, ErrUnsupportedRoute},
		{"wrong method", func(r *ports.ForwardRequest) { r.Method = "post" }, ErrInvalidForwardRequest},
		{"reseller mismatch", func(r *ports.ForwardRequest) { r.Route.ResellerID = "other" }, ErrUnsupportedRoute},
		{"provider mismatch", func(r *ports.ForwardRequest) { r.Route.ProviderType = domain.ProviderGroq }, ErrUnsupportedRoute},
		{"empty route ID", func(r *ports.ForwardRequest) { r.Route.ID = "" }, ErrUnsupportedRoute},
		{"blank client model", func(r *ports.ForwardRequest) { r.Route.ClientModel = " " }, ErrUnsupportedRoute},
		{"unknown rewrite policy", func(r *ports.ForwardRequest) { r.Route.ModelRewritePolicy = "automatic" }, ErrUnsupportedRoute},
		{"models rejected", func(r *ports.ForwardRequest) {
			r.Route.EndpointKind = domain.EndpointModels
			r.Path = modelPath
			r.Method = http.MethodGet
		}, ErrInvalidForwardRequest},
	}
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, "ok"), nil }), StatusClassifier{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := forwardReq(domain.EndpointChat)
			tc.mutate(&req)
			_, err := adapter.Forward(t.Context(), req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

func TestURLSafetyAndRedirectNotFollowed(t *testing.T) {
	var seenURL string
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seenURL = r.URL.String()
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://other.example"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	}), StatusClassifier{})
	req := forwardReq(domain.EndpointChat)
	req.Path = "/v1/chat/completions?provider_option=value"
	_, err := adapter.Forward(t.Context(), req)
	var failure *forwarding.Failure
	if !errors.As(err, &failure) || failure.Kind != forwarding.FailureKindUnexpectedResponse || failure.AttemptState != forwarding.AttemptStateResponseReceived {
		t.Fatalf("failure=%#v err=%v", failure, err)
	}
	if seenURL != "https://provider.example/api/v1/chat/completions?provider_option=value" {
		t.Fatalf("seenURL=%q", seenURL)
	}

	badPaths := []string{"https://attacker.example/path", "//attacker.example/path", "path-without-leading-slash", "/path#fragment", "/path with space"}
	for _, path := range badPaths {
		t.Run(path, func(t *testing.T) {
			req := forwardReq(domain.EndpointChat)
			req.Path = path
			_, err := adapter.Forward(t.Context(), req)
			if !errors.Is(err, ErrUnsupportedRoute) && !errors.Is(err, ErrInvalidUpstreamURL) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestBaseURLWithoutPathJoinsCorrectly(t *testing.T) {
	var seenHost, seenPath string
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seenHost, seenPath = r.URL.Host, r.URL.Path
		return response(200, "ok"), nil
	}), StatusClassifier{}, func(c *Config) { c.Reseller.BaseURL = "https://provider.example" })
	_, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointEmbeddings))
	if err != nil {
		t.Fatal(err)
	}
	if seenHost != "provider.example" || seenPath != "/v1/embeddings" {
		t.Fatalf("host/path=%s %s", seenHost, seenPath)
	}
}

func TestRequestHeadersFilteredAndInputNotMutated(t *testing.T) {
	input := map[string][]string{
		"X-Ordinary": {"a", "b"}, "Content-Type": {"application/json"}, "authorization": {"Bearer " + clientSecret},
		"Proxy-Authorization": {"proxy"}, "X-Service-Token": {"svc"}, "X-Local-Request-ID": {"id"},
		"X-Billing-Token": {"billing"}, "X-Wallet-ID": {"wallet"}, "Connection": {"Foo, Bar"},
		"Transfer-Encoding": {"chunked"}, "TE": {"trailers"}, "Trailer": {"Expires"}, "Upgrade": {"websocket"},
		"Content-Length": {"999"}, "Host": {"evil"}, "Foo": {"remove"}, "Bar": {"remove"},
	}
	original := cloneHeaders(input)
	var got http.Header
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { got = r.Header.Clone(); return response(200, "ok"), nil }), StatusClassifier{})
	req := forwardReq(domain.EndpointChat)
	req.Headers = input
	_, err := adapter.Forward(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("input mutated: %#v", input)
	}
	if got.Get("Authorization") != "Bearer "+resellerSecret {
		t.Fatalf("Authorization=%q", got.Get("Authorization"))
	}
	if got.Get("authorization") == "Bearer "+clientSecret {
		t.Fatal("client secret forwarded")
	}
	for _, name := range []string{"Proxy-Authorization", "X-Service-Token", "X-Local-Request-ID", "X-Billing-Token", "X-Wallet-ID", "Connection", "Transfer-Encoding", "TE", "Trailer", "Upgrade", "Content-Length", "Host", "Foo", "Bar"} {
		if got.Values(name) != nil {
			t.Fatalf("%s forwarded: %#v", name, got.Values(name))
		}
	}
	if !reflect.DeepEqual(got.Values("X-Ordinary"), []string{"a", "b"}) || got.Get("Content-Type") != "application/json" {
		t.Fatalf("ordinary headers lost: %#v", got)
	}
}

func TestBodyPassthroughPolicyNone(t *testing.T) {
	body := []byte("{ \n  \"z\":1.2300e+04, \"model\" : \"client-model\", \"unknown\": true, \"nested\": {\"model\":\"nested\"} }")
	original := append([]byte(nil), body...)
	var got []byte
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got, _ = io.ReadAll(r.Body)
		if len(got) > 0 {
			got[0] = '['
		}
		return response(200, "ok"), nil
	}), StatusClassifier{})
	req := forwardReq(domain.EndpointChat)
	req.Body = body
	_, err := adapter.Forward(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("caller body mutated: %q", body)
	}
	got[0] = original[0]
	if !bytes.Equal(got, original) {
		t.Fatalf("body changed: %q", got)
	}

	req = forwardReq(domain.EndpointChat)
	req.Route.ProviderModel = "provider-model"
	_, err = adapter.Forward(t.Context(), req)
	if !errors.Is(err, ErrUnsupportedRoute) {
		t.Fatalf("err=%v", err)
	}
}

func TestSuccessfulResponseContract(t *testing.T) {
	statuses := []int{200, 201, 204, 299}
	for _, status := range statuses {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := []byte{0, 1, 2, 255, 'o', 'k'}
			closeBody := &closeTrackingBody{Reader: bytes.NewReader(body)}
			classifier := &spyClassifier{}
			adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"X-Upstream": {"a", "b"}}, Body: closeBody}, nil
			}), classifier)
			resp, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != status || !bytes.Equal(resp.Body, body) || !reflect.DeepEqual(resp.Headers["X-Upstream"], []string{"a", "b"}) {
				t.Fatalf("resp=%#v", resp)
			}
			resp.Headers["X-Upstream"][0] = "mutated"
			if closeBody.closed != true || classifier.calls != 0 {
				t.Fatalf("closed=%t classifier=%d", closeBody.closed, classifier.calls)
			}
			if bytes.Contains(resp.Body, []byte(resellerSecret)) || bytes.Contains(resp.Body, []byte(clientSecret)) {
				t.Fatal("secret in response")
			}
		})
	}
}

func TestNonSuccessClassificationAndBoundedBody(t *testing.T) {
	cases := map[int]forwarding.FailureKind{300: forwarding.FailureKindUnexpectedResponse, 307: forwarding.FailureKindUnexpectedResponse, 400: forwarding.FailureKindRequestError, 401: forwarding.FailureKindAuthError, 403: forwarding.FailureKindAuthError, 404: forwarding.FailureKindRequestError, 409: forwarding.FailureKindRequestError, 429: forwarding.FailureKindRateLimited, 500: forwarding.FailureKindServerError, 502: forwarding.FailureKindServerError, 503: forwarding.FailureKindServerError, 599: forwarding.FailureKindServerError}
	for status, kind := range cases {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			closeBody := &closeTrackingBody{Reader: bytes.NewReader([]byte("provider raw error body"))}
			classifier := &spyClassifier{}
			adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"X-Err": {"h"}}, Body: closeBody}, nil
			}), classifier, func(c *Config) { c.MaxResponseBodyBytes = 8 })
			resp, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
			var failure *forwarding.Failure
			if !errors.As(err, &failure) || failure.Kind != kind || failure.AttemptState != forwarding.AttemptStateResponseReceived || !reflect.DeepEqual(resp, ports.ForwardResponse{}) || !closeBody.closed {
				t.Fatalf("resp=%#v failure=%#v err=%v closed=%t", resp, failure, err, closeBody.closed)
			}
			if strings.Contains(fmt.Sprint(err), "provider raw") {
				t.Fatal("raw body leaked")
			}
			if classifier.calls != 1 || string(classifier.body) != "provider" || !classifier.truncated {
				t.Fatalf("classifier=%#v", classifier)
			}
		})
	}
}

func TestRoundTripResponseAndErrorIsResponseReceived(t *testing.T) {
	underlying := errors.New("upstream returned response and error")
	closeBody := &closeTrackingBody{Reader: bytes.NewReader([]byte("provider raw error body"))}
	classifier := &spyClassifier{}
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{"X-Err": {"h"}}, Body: closeBody}, underlying
	}), classifier)

	resp, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
	var failure *forwarding.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected forwarding failure: %v", err)
	}
	if failure.Kind != forwarding.FailureKindServerError || failure.AttemptState != forwarding.AttemptStateResponseReceived || !failure.RouteRetryCandidate {
		t.Fatalf("failure=%#v", failure)
	}
	if !errors.Is(err, underlying) {
		t.Fatalf("underlying cause not wrapped: %v", err)
	}
	if !reflect.DeepEqual(resp, ports.ForwardResponse{}) || !closeBody.closed || classifier.calls != 1 {
		t.Fatalf("resp=%#v closed=%t classifier=%d", resp, closeBody.closed, classifier.calls)
	}
	if strings.Contains(fmt.Sprint(err), "provider raw") {
		t.Fatal("raw body leaked")
	}
}

func TestNetworkFailureAttemptStatesAndSingleAttempt(t *testing.T) {
	underlying := errors.New("network secretless failure")
	t.Run("cancelled before attempt", func(t *testing.T) {
		calls := 0
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, nil }), StatusClassifier{})
		ctx, cancel := contextWithCancel(t)
		cancel()
		_, err := adapter.Forward(ctx, forwardReq(domain.EndpointChat))
		var failure *forwarding.Failure
		if !errors.As(err, &failure) || failure.AttemptState != forwarding.AttemptStateNotSent || !failure.RouteRetryCandidate || calls != 0 {
			t.Fatalf("failure=%#v calls=%d err=%v", failure, calls, err)
		}
	})
	t.Run("before write", func(t *testing.T) {
		calls := 0
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, underlying }), StatusClassifier{})
		_, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
		var failure *forwarding.Failure
		if !errors.As(err, &failure) || failure.AttemptState != forwarding.AttemptStateNotSent || !failure.RouteRetryCandidate || calls != 1 || !errors.Is(err, underlying) {
			t.Fatalf("failure=%#v calls=%d err=%v", failure, calls, err)
		}
	})
	t.Run("after wrote request callback", func(t *testing.T) {
		calls := 0
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			httptrace.ContextClientTrace(r.Context()).WroteRequest(httptrace.WroteRequestInfo{})
			return nil, underlying
		}), StatusClassifier{})
		_, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
		var failure *forwarding.Failure
		if !errors.As(err, &failure) || failure.AttemptState != forwarding.AttemptStateSentNoResponse || failure.RouteRetryCandidate || calls != 1 {
			t.Fatalf("failure=%#v calls=%d err=%v", failure, calls, err)
		}
	})
	t.Run("after body read", func(t *testing.T) {
		calls := 0
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			_, _ = r.Body.Read(make([]byte, 1))
			return nil, underlying
		}), StatusClassifier{})
		_, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
		var failure *forwarding.Failure
		if !errors.As(err, &failure) || failure.AttemptState != forwarding.AttemptStateSentNoResponse || failure.RouteRetryCandidate || calls != 1 {
			t.Fatalf("failure=%#v calls=%d err=%v", failure, calls, err)
		}
	})
}

func TestResponseSizeLimit(t *testing.T) {
	t.Run("success exactly limit accepted", func(t *testing.T) {
		closeBody := &closeTrackingBody{Reader: bytes.NewReader([]byte("12345"))}
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: closeBody}, nil
		}), StatusClassifier{}, func(c *Config) { c.MaxResponseBodyBytes = 5 })
		resp, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
		if err != nil || string(resp.Body) != "12345" || !closeBody.closed {
			t.Fatalf("resp=%#v err=%v closed=%t", resp, err, closeBody.closed)
		}
	})
	t.Run("success overflow rejected", func(t *testing.T) {
		closeBody := &closeTrackingBody{Reader: bytes.NewReader([]byte("123456"))}
		adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: closeBody}, nil
		}), StatusClassifier{}, func(c *Config) { c.MaxResponseBodyBytes = 5 })
		resp, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
		var failure *forwarding.Failure
		if !errors.Is(err, ErrUpstreamResponseTooLarge) || !errors.As(err, &failure) || failure.Kind != forwarding.FailureKindResponseTooLarge || !reflect.DeepEqual(resp, ports.ForwardResponse{}) || !closeBody.closed {
			t.Fatalf("resp=%#v failure=%#v err=%v closed=%t", resp, failure, err, closeBody.closed)
		}
	})
	t.Run("non-success exactly and overflow", func(t *testing.T) {
		for _, body := range []string{"12345", "123456"} {
			classifier := &spyClassifier{}
			closeBody := &closeTrackingBody{Reader: bytes.NewReader([]byte(body))}
			adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 500, Body: closeBody}, nil
			}), classifier, func(c *Config) { c.MaxResponseBodyBytes = 5 })
			_, err := adapter.Forward(t.Context(), forwardReq(domain.EndpointChat))
			if err == nil || !closeBody.closed || classifier.truncated != (len(body) > 5) {
				t.Fatalf("body=%q err=%v closed=%t classifier=%#v", body, err, closeBody.closed, classifier)
			}
		}
	})
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func contextWithCancel(t *testing.T) (context.Context, func()) {
	t.Helper()
	return context.WithCancel(t.Context())
}
