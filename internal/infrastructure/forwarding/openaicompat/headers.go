package openaicompat

import (
	"net/http"
	"strings"
)

func buildUpstreamHeaders(input map[string][]string, resellerAPIKey string) http.Header {
	blocked := map[string]struct{}{
		"authorization":       {},
		"proxy-authorization": {},
		"x-service-token":     {},
		"x-local-request-id":  {},
		"connection":          {},
		"proxy-connection":    {},
		"keep-alive":          {},
		"transfer-encoding":   {},
		"te":                  {},
		"trailer":             {},
		"upgrade":             {},
		"content-length":      {},
		"host":                {},
	}
	for name, values := range input {
		if strings.EqualFold(name, "Connection") {
			for _, value := range values {
				for _, token := range strings.Split(value, ",") {
					if trimmed := strings.TrimSpace(token); trimmed != "" {
						blocked[strings.ToLower(trimmed)] = struct{}{}
					}
				}
			}
		}
	}

	out := make(http.Header)
	for name, values := range input {
		lower := strings.ToLower(name)
		if _, found := blocked[lower]; found {
			continue
		}
		if strings.HasPrefix(lower, "x-billing-") || strings.HasPrefix(lower, "x-wallet-") {
			continue
		}
		copied := append([]string(nil), values...)
		out[name] = copied
	}
	out.Set("Authorization", "Bearer "+resellerAPIKey)
	return out
}

func cloneHeaders(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	out := make(map[string][]string, len(input))
	for name, values := range input {
		out[name] = append([]string(nil), values...)
	}
	return out
}
