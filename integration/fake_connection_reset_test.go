//go:build integration

package integration_test

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFakeServiceConnectionResetScenario(t *testing.T) {
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
		_ = conn.Close()
	}()

	client := &http.Client{
		Timeout: time.Second,
	}

	response, err := client.Get("http://" + listener.Addr().String() + "/connection-reset")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("expected connection reset error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "reset") &&
		!strings.Contains(strings.ToLower(err.Error()), "eof") {
		t.Fatalf("error=%v, want connection reset/eof", err)
	}

	<-done
}
