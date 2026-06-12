package httptransport

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type closeTrackingBody struct {
	reader io.Reader
	closed bool
}

func (b *closeTrackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *closeTrackingBody) Close() error               { b.closed = true; return nil }

type failingBody struct {
	err    error
	closed bool
}

func (b *failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b *failingBody) Close() error             { b.closed = true; return nil }

func TestReadBodyLimited(t *testing.T) {
	t.Run("body exactly at limit accepted and closed", func(t *testing.T) {
		body := &closeTrackingBody{reader: strings.NewReader("12345")}
		request := httptest.NewRequest("POST", "/", nil)
		request.Body = body
		got, err := ReadBodyLimited(request, 5)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "12345" {
			t.Fatalf("body = %q", got)
		}
		if !body.closed {
			t.Fatal("body was not closed")
		}
	})

	t.Run("body larger than limit rejected", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/", strings.NewReader("123456"))
		_, err := ReadBodyLimited(request, 5)
		if !errors.Is(err, ErrRequestBodyTooLarge) {
			t.Fatalf("err = %v, want ErrRequestBodyTooLarge", err)
		}
	})

	t.Run("invalid limit rejected", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/", strings.NewReader("x"))
		if _, err := ReadBodyLimited(request, 0); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("body read error preserved", func(t *testing.T) {
		readErr := errors.New("reader failed")
		body := &failingBody{err: readErr}
		request := httptest.NewRequest("POST", "/", nil)
		request.Body = body
		_, err := ReadBodyLimited(request, 10)
		if !errors.Is(err, readErr) {
			t.Fatalf("err = %v, want %v", err, readErr)
		}
		if !body.closed {
			t.Fatal("body was not closed")
		}
	})
}

func TestReadJSONBodyLimited(t *testing.T) {
	raw := []byte("{ \"b\" : [2, 1], \"a\" : true }\n")
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     error
	}{
		{name: "missing Content-Type accepted", body: string(raw)},
		{name: "application/json accepted", contentType: "application/json", body: string(raw)},
		{name: "application/json charset accepted", contentType: "application/json; charset=utf-8", body: string(raw)},
		{name: "text/plain rejected", contentType: "text/plain", body: string(raw), wantErr: ErrUnsupportedContentType},
		{name: "malformed Content-Type rejected", contentType: "application/json; charset", body: string(raw), wantErr: ErrUnsupportedContentType},
		{name: "invalid JSON rejected", contentType: "application/json", body: "{invalid", wantErr: ErrInvalidJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			got, err := ReadJSONBodyLimited(request, 100)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.body {
				t.Fatalf("raw body changed: got %q want %q", got, tt.body)
			}
		})
	}
}
