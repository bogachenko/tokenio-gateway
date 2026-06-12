package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const validChargeID = "billchg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type captureRoundTripper struct {
	requests []*http.Request
	bodies   []string
	response *http.Response
	err      error
}

func (rt *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req)
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		rt.bodies = append(rt.bodies, string(body))
	}
	if rt.err != nil {
		return nil, rt.err
	}
	if rt.response != nil {
		return rt.response, nil
	}
	return jsonResp(200, `{"currency":"RUB","balance_cents":100}`), nil
}

func TestBalanceCredentialBoundaryAndSingleRoundTrip(t *testing.T) {
	rt := &captureRoundTripper{response: jsonResp(200, `{"currency":"RUB","balance_cents":10000}`)}
	client := newTestClient(t, rt)
	balance, err := client.GetBalance(t.Context(), "billing-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Currency != "RUB" || balance.BalanceCents != 10000 {
		t.Fatalf("balance=%+v", balance)
	}
	if len(rt.requests) != 1 {
		t.Fatalf("round trips=%d", len(rt.requests))
	}
	req := rt.requests[0]
	if req.Method != http.MethodGet || req.URL.Path != "/root/api/v1/wallet/balance" {
		t.Fatalf("bad request %s %s", req.Method, req.URL.String())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer billing-jwt" {
		t.Fatalf("Authorization=%q", got)
	}
	if got := req.Header.Get("X-Service-Token"); got != "" {
		t.Fatalf("service token leaked to balance: %q", got)
	}
}

func TestChargeCredentialBoundaryAndBody(t *testing.T) {
	remoteBalance := int64(900)
	rt := &captureRoundTripper{response: jsonResp(200, fmt.Sprintf(`{"balance_cents":%d}`, remoteBalance))}
	client := newTestClient(t, rt)
	result, err := client.Charge(t.Context(), ports.BillingChargeRequest{RequestID: validChargeID, UserID: "billing-user", Model: "openai:gpt", InputTokens: 3, OutputTokens: 4, AmountCents: 50, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if result.BalanceCents == nil || *result.BalanceCents != remoteBalance {
		t.Fatalf("result=%+v", result)
	}
	if len(rt.requests) != 1 {
		t.Fatalf("round trips=%d", len(rt.requests))
	}
	req := rt.requests[0]
	if req.Method != http.MethodPost || req.URL.Path != "/root/api/v1/usage/charge" {
		t.Fatalf("bad request %s %s", req.Method, req.URL.String())
	}
	if req.Header.Get("X-Service-Token") != "svc-secret" {
		t.Fatalf("service token missing")
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("jwt leaked to charge: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("Idempotency-Key") != validChargeID {
		t.Fatalf("idempotency key missing")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rt.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["user_id"] != "billing-user" {
		t.Fatalf("body user=%v", body["user_id"])
	}
}

func TestBaseURLValidation(t *testing.T) {
	for _, raw := range []string{"", "http://", "ftp://billing", "https://billing?x=1", "https://billing#frag", "https://user:password@billing.example"} {
		if _, err := New(Config{BaseURL: raw, ServiceToken: "token", RoundTripper: &captureRoundTripper{}, MaxResponseBodyBytes: 100}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%q err=%v", raw, err)
		}
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	rt := &captureRoundTripper{response: &http.Response{StatusCode: http.StatusFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("redirect secret"))}}
	client := newTestClient(t, rt)
	err := func() error { _, err := client.GetBalance(t.Context(), "jwt"); return err }()
	if !errors.Is(err, ErrBillingHTTPStatus) {
		t.Fatalf("err=%v", err)
	}
	if len(rt.requests) != 1 {
		t.Fatalf("round trips=%d", len(rt.requests))
	}
}

func TestRawBillingErrorNotLeakedAndSingleJSON(t *testing.T) {
	rt := &captureRoundTripper{response: jsonResp(500, `raw-secret-body`)}
	client := newTestClient(t, rt)
	_, err := client.Charge(t.Context(), ports.BillingChargeRequest{RequestID: validChargeID, UserID: "u", Model: "m", AmountCents: 1, Currency: "RUB"})
	if !errors.Is(err, ErrBillingHTTPStatus) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "raw-secret-body") {
		t.Fatal("raw body leaked")
	}
	rt.response = jsonResp(200, `{"currency":"RUB","balance_cents":1} {}`)
	_, err = client.GetBalance(t.Context(), "jwt")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("single json err=%v", err)
	}
}

func TestBalanceResponseValidation(t *testing.T) {
	for _, body := range []string{`{"currency":"USD","balance_cents":1}`, `{"currency":"RUB","balance_cents":-1}`} {
		rt := &captureRoundTripper{response: jsonResp(200, body)}
		client := newTestClient(t, rt)
		_, err := client.GetBalance(t.Context(), "jwt")
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("body %s err=%v", body, err)
		}
	}
}

func newTestClient(t *testing.T, rt *captureRoundTripper) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: "https://billing.example/root/", ServiceToken: "svc-secret", RoundTripper: rt, MaxResponseBodyBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientFormattingDoesNotLeakServiceToken(t *testing.T) {
	secret := "svc-format-secret"
	client, err := New(Config{BaseURL: "https://billing.example", ServiceToken: secret, RoundTripper: &captureRoundTripper{}, MaxResponseBodyBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, formatted := range []string{fmt.Sprintf("%+v", client), fmt.Sprintf("%#v", client)} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("service token leaked through formatting: %s", formatted)
		}
	}
}

type closeTrackingBody struct{ closed bool }

func (b *closeTrackingBody) Read(p []byte) (int, error) { return 0, io.EOF }
func (b *closeTrackingBody) Close() error               { b.closed = true; return nil }

type responseErrorRoundTripper struct{ body *closeTrackingBody }

func (rt *responseErrorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusBadGateway, Body: rt.body, Header: make(http.Header)}, errors.New("network with response")
}

func TestRoundTripErrorClosesResponseBodies(t *testing.T) {
	for _, call := range []struct {
		name string
		run  func(*Client) error
	}{
		{name: "balance", run: func(client *Client) error { _, err := client.GetBalance(t.Context(), "jwt"); return err }},
		{name: "charge", run: func(client *Client) error {
			_, err := client.Charge(t.Context(), ports.BillingChargeRequest{RequestID: validChargeID, UserID: "u", Model: "m", AmountCents: 1, Currency: "RUB"})
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			body := &closeTrackingBody{}
			client, err := New(Config{BaseURL: "https://billing.example", ServiceToken: "token", RoundTripper: &responseErrorRoundTripper{body: body}, MaxResponseBodyBytes: 100})
			if err != nil {
				t.Fatal(err)
			}
			if err := call.run(client); err == nil {
				t.Fatal("expected error")
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}

func TestChargeRequestIDMustBeStableBatchID(t *testing.T) {
	client := newTestClient(t, &captureRoundTripper{})
	_, err := client.Charge(t.Context(), ports.BillingChargeRequest{RequestID: "batch-1", UserID: "u", Model: "m", AmountCents: 1, Currency: "RUB"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestTransportErrorsAreNormalizedAndDoNotLeakCredentials(t *testing.T) {
	secretErr := errors.New("dump Authorization: Bearer billing-jwt X-Service-Token svc-secret request payload")
	for _, tc := range []struct {
		name string
		run  func(*Client) error
	}{
		{name: "balance", run: func(client *Client) error { _, err := client.GetBalance(t.Context(), "billing-jwt"); return err }},
		{name: "charge", run: func(client *Client) error {
			_, err := client.Charge(t.Context(), ports.BillingChargeRequest{RequestID: validChargeID, UserID: "billing-user", Model: "openai:gpt", AmountCents: 1, Currency: "RUB"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, &captureRoundTripper{err: secretErr})
			err := tc.run(client)
			if !errors.Is(err, ErrBillingTransport) {
				t.Fatalf("err=%v", err)
			}
			formatted := fmt.Sprintf("%+v", err)
			for _, secret := range []string{"billing-jwt", "svc-secret", "Authorization", "X-Service-Token", "payload"} {
				if strings.Contains(formatted, secret) {
					t.Fatalf("transport error leaked %q: %s", secret, formatted)
				}
			}
		})
	}
}
