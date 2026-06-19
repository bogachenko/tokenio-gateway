//go:build integration

package integration_test

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFakeServiceHeadersReceivedBodyFailedScenario(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		_, _ = conn.Write([]byte(
			"HTTP/1.1 200 OK\r\n" +
				"Content-Type: application/json\r\n" +
				"Content-Length: 100\r\n" +
				"Connection: close\r\n" +
				"\r\n" +
				`{"partial":`,
		))
	}()

	client := &http.Client{
		Timeout: time.Second,
	}

	response, err := client.Get("http://" + listener.Addr().String() + "/body-failed")
	if err != nil {
		t.Fatalf("request failed before headers: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err == nil {
		t.Fatalf("expected body read error, body=%q", string(body))
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unexpected eof") {
		t.Fatalf("error=%v, want unexpected EOF", err)
	}

	<-done
}
