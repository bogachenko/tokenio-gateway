package domain

import (
	"errors"
	"net/url"
	"strings"
)

var ErrInvalidResellerBaseURL = errors.New("invalid reseller base URL")

func ValidateResellerBaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrInvalidResellerBaseURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidResellerBaseURL
	}
	if !parsed.IsAbs() ||
		parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return ErrInvalidResellerBaseURL
	}
	return nil
}
