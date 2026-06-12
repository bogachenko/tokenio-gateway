package openaicompat

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

func parseBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w", ErrInvalidAdapterConfig)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w", ErrInvalidAdapterConfig)
	}
	if !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w", ErrInvalidAdapterConfig)
	}
	return parsed, nil
}

func buildUpstreamURL(base *url.URL, requestPath string) (*url.URL, error) {
	if requestPath == "" || requestPath[0] != '/' || strings.HasPrefix(requestPath, "//") || strings.Contains(requestPath, "#") || hasControlOrSpace(requestPath) {
		return nil, fmt.Errorf("%w", ErrInvalidUpstreamURL)
	}
	parsed, err := url.ParseRequestURI(requestPath)
	if err != nil {
		return nil, fmt.Errorf("%w", ErrInvalidUpstreamURL)
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" || parsed.Path == "" || parsed.Path[0] != '/' {
		return nil, fmt.Errorf("%w", ErrInvalidUpstreamURL)
	}
	joined := *base
	basePath := strings.TrimRight(joined.EscapedPath(), "/")
	if basePath == "" {
		joined.Path = parsed.Path
	} else {
		joined.Path = path.Join(basePath, parsed.Path)
		if strings.HasSuffix(parsed.Path, "/") && !strings.HasSuffix(joined.Path, "/") {
			joined.Path += "/"
		}
	}
	joined.RawQuery = parsed.RawQuery
	joined.Fragment = ""
	joined.User = nil
	return &joined, nil
}

func hasControlOrSpace(s string) bool {
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if r <= 0x20 || r == 0x7f {
			return true
		}
		s = s[size:]
	}
	return false
}
