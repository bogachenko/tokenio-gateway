package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

var ErrAPIKeyGeneration = errors.New("api key generation failed")

type SecureAPIKeyGenerator struct{ reader io.Reader }

func NewSecureAPIKeyGenerator() *SecureAPIKeyGenerator {
	return &SecureAPIKeyGenerator{reader: rand.Reader}
}
func NewSecureAPIKeyGeneratorWithReader(reader io.Reader) (*SecureAPIKeyGenerator, error) {
	if reader == nil {
		return nil, ErrAPIKeyGeneration
	}
	return &SecureAPIKeyGenerator{reader: reader}, nil
}
func (g *SecureAPIKeyGenerator) GenerateAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(g.reader, buf); err != nil {
		return "", ErrAPIKeyGeneration
	}
	return "sk_live_" + base64.RawURLEncoding.EncodeToString(buf), nil
}
