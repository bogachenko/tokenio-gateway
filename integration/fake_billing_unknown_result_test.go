//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fb "github.com/bogachenko/tokenio-gateway/integration/fakes/billing"
)

func TestFakeBillingUnknownResultScenario(t *testing.T) {
	t.Parallel()

	server := fb.New()
	defer server.Close()

	server.SetResponse(fb.Response{
		Status: http.StatusOK,
		Header: unknownBillingJSONHeader(),
		Body: []byte(`{
			"status":"unknown",
			"request_id":"billing-unknown-1",
			"charged_amount":"0.00"
		}`),
	})

	response, err := http.Post(
		server.URL()+"/v1/charges",
		"application/json",
		strings.NewReader(`{"request_id":"billing-unknown-1","requested_amount":"10.00"}`),
	)
	if err != nil {
		t.Fatalf("post unknown billing result: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	compact := compactUnknownBillingJSON(string(body))
	for _, want := range []string{`"status":"unknown"`, `"request_id":"billing-unknown-1"`, `"charged_amount":"0.00"`} {
		if !strings.Contains(compact, want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if !strings.Contains(string(requests[0].Body), `"request_id":"billing-unknown-1"`) {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func unknownBillingJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func compactUnknownBillingJSON(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\t", "")
	return value
}
