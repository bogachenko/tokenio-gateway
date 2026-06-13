package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestSecureAPIKeyGeneratorUsesThirtyTwoRandomBytes(t *testing.T) {
	generator, err := NewSecureAPIKeyGeneratorWithReader(bytes.NewReader(bytes.Repeat([]byte{0x7f}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := generator.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "sk_live_"
	decoded, err := base64.RawURLEncoding.DecodeString(raw[len(prefix):])
	if err != nil || len(decoded) != 32 {
		t.Fatalf("raw=%q decoded=%d err=%v", raw, len(decoded), err)
	}
}

func TestSecureAPIKeyGeneratorDoesNotReturnPartialKey(t *testing.T) {
	generator, err := NewSecureAPIKeyGeneratorWithReader(failingReader{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := generator.GenerateAPIKey()
	if !errors.Is(err, ErrAPIKeyGeneration) || raw != "" {
		t.Fatalf("raw=%q err=%v", raw, err)
	}
}
