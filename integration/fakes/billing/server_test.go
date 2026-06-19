//go:build integration

package fakebilling

import (
	"io"
	"net/http"
	"testing"
)

func TestFakeBillingRecordsRequestsAndReturnsConfiguredResponse(t *testing.T) {
	server := New()
	defer server.Close()

	server.SetResponse(Response{
		Status: http.StatusPaymentRequired,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: []byte(`{"error":"quota_exhausted"}`),
	})

	response, err := http.Post(server.URL()+"/v1/charges", "application/json", http.NoBody)
	if err != nil {
		t.Fatalf("post fake billing: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if string(body) != `{"error":"quota_exhausted"}` {
		t.Fatalf("body=%q", string(body))
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/v1/charges" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
}
