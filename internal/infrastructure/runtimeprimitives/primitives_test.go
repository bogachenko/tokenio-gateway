package runtimeprimitives

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type failingEntropyReader struct {
	err error
}

func (r failingEntropyReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestUTCClockReturnsNonZeroUTC(t *testing.T) {
	clock := NewUTCClock()
	now := clock.Now()

	if now.IsZero() {
		t.Fatal("clock returned zero time")
	}
	if now.Location() != time.UTC {
		t.Fatalf("clock location = %v, want UTC", now.Location())
	}
}

func TestSecureRequestIDGeneratorContract(t *testing.T) {
	entropy := make([]byte, requestIDEntropyBytes*3)
	for index := range entropy {
		entropy[index] = byte(index)
	}

	generator, err := newSecureRequestIDGenerator(
		bytes.NewReader(entropy),
	)
	if err != nil {
		t.Fatalf("newSecureRequestIDGenerator: %v", err)
	}

	tests := []struct {
		name   string
		prefix string
		create func() (string, error)
	}{
		{
			name:   "public",
			prefix: "llmreq_",
			create: generator.NewLocalRequestID,
		},
		{
			name:   "admin",
			prefix: "admreq_",
			create: generator.NewAdminRequestID,
		},
		{
			name:   "provisioning",
			prefix: "provreq_",
			create: generator.NewProvisioningRequestID,
		},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestID, err := test.create()
			if err != nil {
				t.Fatalf("generate request ID: %v", err)
			}
			if !strings.HasPrefix(requestID, test.prefix) {
				t.Fatalf(
					"request ID %q does not have prefix %q",
					requestID,
					test.prefix,
				)
			}
			wantLength := len(test.prefix) + requestIDEntropyBytes*2
			if len(requestID) != wantLength {
				t.Fatalf(
					"request ID length = %d, want %d",
					len(requestID),
					wantLength,
				)
			}
			if _, duplicate := seen[requestID]; duplicate {
				t.Fatalf("duplicate request ID %q", requestID)
			}
			seen[requestID] = struct{}{}
		})
	}
}

func TestSecureRequestIDGeneratorPropagatesEntropyFailure(t *testing.T) {
	entropyErr := errors.New("entropy unavailable")
	generator, err := newSecureRequestIDGenerator(
		failingEntropyReader{err: entropyErr},
	)
	if err != nil {
		t.Fatalf("newSecureRequestIDGenerator: %v", err)
	}

	requestID, err := generator.NewLocalRequestID()
	if requestID != "" {
		t.Fatalf("request ID = %q, want empty", requestID)
	}
	if !errors.Is(err, ErrRequestIDGeneration) {
		t.Fatalf("error = %v, want ErrRequestIDGeneration", err)
	}
	if !errors.Is(err, entropyErr) {
		t.Fatalf("error = %v, want entropy cause", err)
	}
}

func TestSecureRequestIDGeneratorRejectsNilEntropy(t *testing.T) {
	generator, err := newSecureRequestIDGenerator(nil)
	if generator != nil {
		t.Fatal("generator must be nil")
	}
	if !errors.Is(err, ErrRequestIDGeneration) {
		t.Fatalf("error = %v, want ErrRequestIDGeneration", err)
	}
}

func TestSecureRequestIDGeneratorRejectsShortEntropy(t *testing.T) {
	generator, err := newSecureRequestIDGenerator(
		io.LimitReader(
			bytes.NewReader(make([]byte, requestIDEntropyBytes)),
			3,
		),
	)
	if err != nil {
		t.Fatalf("newSecureRequestIDGenerator: %v", err)
	}

	_, err = generator.NewAdminRequestID()
	if !errors.Is(err, ErrRequestIDGeneration) {
		t.Fatalf("error = %v, want ErrRequestIDGeneration", err)
	}
}
