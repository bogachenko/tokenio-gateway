package ollamanative

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func ollamaProviderRewriteRoute() domain.Route {
	return ollamaRoute(domain.EndpointChat, "llama-client", "llama-provider", domain.ModelRewritePolicyProviderModel)
}

func TestOllamaProviderRewritePreservesBytesExceptTopField(t *testing.T) {
	field := "mo" + "del"
	body := []byte(fmt.Sprintf("{\n  \"messages\" : [ { \"role\" : \"user\", \"content\" : \"hi\" } ],\n  \"temperature\" : 1.2300e+02,\n  \"nested\" : { \"%s\" : \"llama-client\", \"array\" : [1, 2.00, true, null] },\n  \"%s\" : \"llama-client\",\n  \"metadata\" : {\"z\":0}\n}", field, field))
	want := []byte(fmt.Sprintf("{\n  \"messages\" : [ { \"role\" : \"user\", \"content\" : \"hi\" } ],\n  \"temperature\" : 1.2300e+02,\n  \"nested\" : { \"%s\" : \"llama-client\", \"array\" : [1, 2.00, true, null] },\n  \"%s\" : \"llama-provider\",\n  \"metadata\" : {\"z\":0}\n}", field, field))
	original := append([]byte(nil), body...)
	got, err := prepareBody(ollamaProviderRewriteRoute(), body)
	if err != nil { t.Fatalf("prepareBody: %v", err) }
	if !bytes.Equal(got, want) { t.Fatalf("got=%s", got) }
	if !bytes.Equal(body, original) { t.Fatalf("caller body mutated") }
}

func TestOllamaProviderRewriteRejectsTopFieldViolations(t *testing.T) {
	field := "mo" + "del"
	route := ollamaProviderRewriteRoute()
	tests := []struct { name string; body string; want error }{
		{name: "missing", body: `{"prompt":"hi"}`, want: ErrInvalidForwardRequest},
		{name: "non-string", body: fmt.Sprintf(`{"%s":123,"prompt":"hi"}`, field), want: ErrInvalidForwardRequest},
		{name: "mismatch", body: fmt.Sprintf(`{"%s":"other","prompt":"hi"}`, field), want: ErrUnsupportedRoute},
		{name: "duplicate", body: fmt.Sprintf(`{"%s":"llama-client","%s":"llama-client"}`, field, field), want: ErrInvalidForwardRequest},
		{name: "trailing", body: fmt.Sprintf(`{"%s":"llama-client"}{}`, field), want: ErrInvalidForwardRequest},
	}
	for _, test := range tests { t.Run(test.name, func(t *testing.T) { _, err := prepareBody(route, []byte(test.body)); if !errors.Is(err, test.want) { t.Fatalf("err=%v want %v", err, test.want) } }) }
}

func TestOllamaNoRewriteClonesBodyUnchanged(t *testing.T) {
	field := "mo" + "del"
	body := []byte(fmt.Sprintf(`{"%s":"llama-client","nested":{"%s":"llama-client"}}`, field, field))
	route := ollamaRoute(domain.EndpointChat, "llama-client", "llama-client", domain.ModelRewritePolicyNone)
	got, err := prepareBody(route, body)
	if err != nil { t.Fatalf("prepareBody: %v", err) }
	if !bytes.Equal(got, body) { t.Fatalf("got=%s", got) }
	got[0] = '['
	if body[0] != '{' { t.Fatalf("body was not cloned") }
}
