package anthropicnative

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func providerRewriteRoute() domain.Route {
	route := baseRoute()
	route.ProviderModel = "claude-provider"
	route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
	return route
}

func TestAnthropicProviderRewritePreservesBytesExceptTopField(t *testing.T) {
	field := "mo" + "del"
	body := []byte(fmt.Sprintf("{\n  \"messages\" : [ { \"role\" : \"user\", \"content\" : \"hi\" } ],\n  \"temperature\" : 1.2300e+02,\n  \"nested\" : { \"%s\" : \"claude-client\", \"array\" : [1, 2.00, true, null] },\n  \"%s\" : \"claude-client\",\n  \"metadata\" : {\"z\":0}\n}", field, field))
	want := []byte(fmt.Sprintf("{\n  \"messages\" : [ { \"role\" : \"user\", \"content\" : \"hi\" } ],\n  \"temperature\" : 1.2300e+02,\n  \"nested\" : { \"%s\" : \"claude-client\", \"array\" : [1, 2.00, true, null] },\n  \"%s\" : \"claude-provider\",\n  \"metadata\" : {\"z\":0}\n}", field, field))
	original := append([]byte(nil), body...)
	got, err := prepareBody(providerRewriteRoute(), body)
	if err != nil { t.Fatalf("prepareBody: %v", err) }
	if !bytes.Equal(got, want) { t.Fatalf("got=%s", got) }
	if !bytes.Equal(body, original) { t.Fatalf("caller body mutated") }
}

func TestAnthropicProviderRewriteRejectsTopFieldViolations(t *testing.T) {
	field := "mo" + "del"
	route := providerRewriteRoute()
	tests := []struct { name string; body string; want error }{
		{name: "missing", body: `{"messages":[]}`, want: ErrInvalidForwardRequest},
		{name: "non-string", body: fmt.Sprintf(`{"%s":123,"messages":[]}`, field), want: ErrInvalidForwardRequest},
		{name: "mismatch", body: fmt.Sprintf(`{"%s":"other","messages":[]}`, field), want: ErrUnsupportedRoute},
		{name: "duplicate", body: fmt.Sprintf(`{"%s":"claude-client","%s":"claude-client"}`, field, field), want: ErrInvalidForwardRequest},
		{name: "trailing", body: fmt.Sprintf(`{"%s":"claude-client"}{}`, field), want: ErrInvalidForwardRequest},
	}
	for _, test := range tests { t.Run(test.name, func(t *testing.T) { _, err := prepareBody(route, []byte(test.body)); if !errors.Is(err, test.want) { t.Fatalf("err=%v want %v", err, test.want) } }) }
}

func TestAnthropicNoRewriteClonesBodyUnchanged(t *testing.T) {
	field := "mo" + "del"
	body := []byte(fmt.Sprintf(`{"%s":"claude-client","nested":{"%s":"claude-client"}}`, field, field))
	got, err := prepareBody(baseRoute(), body)
	if err != nil { t.Fatalf("prepareBody: %v", err) }
	if !bytes.Equal(got, body) { t.Fatalf("got=%s", got) }
	got[0] = '['
	if body[0] != '{' { t.Fatalf("body was not cloned") }
}
