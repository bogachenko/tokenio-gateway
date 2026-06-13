package runtimeprimitives

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const requestIDEntropyBytes = 16

var ErrRequestIDGeneration = errors.New("request ID generation failed")

type UTCClock struct{}

func NewUTCClock() *UTCClock {
	return &UTCClock{}
}

func (*UTCClock) Now() time.Time {
	return time.Now().UTC()
}

type SecureRequestIDGenerator struct {
	entropy io.Reader
}

func NewSecureRequestIDGenerator() *SecureRequestIDGenerator {
	return &SecureRequestIDGenerator{entropy: rand.Reader}
}

func newSecureRequestIDGenerator(
	entropy io.Reader,
) (*SecureRequestIDGenerator, error) {
	if entropy == nil {
		return nil, fmt.Errorf(
			"%w: entropy source is nil",
			ErrRequestIDGeneration,
		)
	}
	return &SecureRequestIDGenerator{entropy: entropy}, nil
}

func (g *SecureRequestIDGenerator) NewLocalRequestID() (string, error) {
	return g.newID("llmreq_")
}

func (g *SecureRequestIDGenerator) NewAdminRequestID() (string, error) {
	return g.newID("admreq_")
}

func (g *SecureRequestIDGenerator) NewProvisioningRequestID() (string, error) {
	return g.newID("provreq_")
}

func (g *SecureRequestIDGenerator) newID(prefix string) (string, error) {
	if g == nil || g.entropy == nil {
		return "", fmt.Errorf(
			"%w: entropy source is unavailable",
			ErrRequestIDGeneration,
		)
	}

	entropy := make([]byte, requestIDEntropyBytes)
	if _, err := io.ReadFull(g.entropy, entropy); err != nil {
		return "", fmt.Errorf(
			"%w: read entropy: %w",
			ErrRequestIDGeneration,
			err,
		)
	}
	return prefix + hex.EncodeToString(entropy), nil
}

var _ ports.Clock = (*UTCClock)(nil)
var _ ports.RequestIDGenerator = (*SecureRequestIDGenerator)(nil)
