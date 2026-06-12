package openaicompat

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestModelRewriteBytePreserving(t *testing.T) {
	body := []byte("{\n  \"prefix\": 1.2300e+04, \"model\" : \"client-model\", \"metadata\": {\"model\":\"nested\"}, \"suffix\": true\n}")
	original := append([]byte(nil), body...)
	var got []byte
	adapter := newTestAdapter(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got, _ = io.ReadAll(r.Body)
		return response(200, "ok"), nil
	}), StatusClassifier{})
	req := forwardReq(domain.EndpointChat)
	req.Body = body
	req.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
	req.Route.ProviderModel = "provider \"model\" \\ one"
	_, err := adapter.Forward(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, original) {
		t.Fatal("input body mutated")
	}
	wantValue := "\"provider \\\"model\\\" \\\\ one\""
	prefix := []byte("{\n  \"prefix\": 1.2300e+04, \"model\" : ")
	suffix := []byte(", \"metadata\": {\"model\":\"nested\"}, \"suffix\": true\n}")
	if !bytes.Equal(got[:len(prefix)], prefix) || !bytes.Equal(got[len(got)-len(suffix):], suffix) || string(got[len(prefix):len(got)-len(suffix)]) != wantValue {
		t.Fatalf("rewrite not byte-preserving: %q", got)
	}
	if strings.Count(string(got), "nested") != 1 {
		t.Fatalf("nested changed: %q", got)
	}
}

func TestModelRewriteValidation(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		client   string
		provider string
	}{
		{"missing model", `{"x":1}`, "client-model", "provider"},
		{"blank model", `{"model":""}`, "client-model", "provider"},
		{"non-string model", `{"model":123}`, "client-model", "provider"},
		{"duplicate top-level", `{"model":"client-model","model":"client-model"}`, "client-model", "provider"},
		{"escaped key", `{"\u006dodel":"client-model"}`, "other", "provider"},
		{"duplicate escaped plain", `{"\u006dodel":"client-model","model":"client-model"}`, "client-model", "provider"},
		{"go hex escape rejected", `{"model":"client\x41"}`, "clientA", "provider"},
		{"go bell escape rejected", `{"model":"client\a"}`, "client", "provider"},
		{"invalid escaped key rejected", `{"\x6dodel":"client-model"}`, "client-model", "provider"},
		{"array root", `[{"model":"client-model"}]`, "client-model", "provider"},
		{"scalar root", `123`, "client-model", "provider"},
		{"invalid JSON", `{"model":"client-model",`, "client-model", "provider"},
		{"trailing JSON data", `{"model":"client-model"} {}`, "client-model", "provider"},
		{"client model mismatch", `{"model":"actual"}`, "client-model", "provider"},
		{"blank provider model", `{"model":"client-model"}`, "client-model", " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rewriteTopLevelModel([]byte(tc.body), tc.client, tc.provider)
			if !errors.Is(err, ErrModelRewriteFailed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestModelRewriteUsesJSONStringEscaping(t *testing.T) {
	got, err := rewriteTopLevelModel([]byte(`{"model":"client-model"}`), "client-model", "provider-\u0001-model")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"model":"provider-\u0001-model"}` {
		t.Fatalf("got %q", got)
	}
}

func TestEscapedModelKeyRecognized(t *testing.T) {
	got, err := rewriteTopLevelModel([]byte(`{"\u006dodel":"client-model","x":{"model":"nested"}}`), "client-model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"\u006dodel":"provider","x":{"model":"nested"}}` {
		t.Fatalf("got %q", got)
	}
}
