package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

var (
	ErrRequestBodyTooLarge    = errors.New("request body too large")
	ErrUnsupportedContentType = errors.New("unsupported content type")
	ErrInvalidJSON            = errors.New("invalid json")
)

func ReadBodyLimited(request *http.Request, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("body limit must be positive")
	}
	defer request.Body.Close()

	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrRequestBodyTooLarge
	}
	return body, nil
}

func ReadJSONBodyLimited(request *http.Request, limit int64) ([]byte, error) {
	contentType := request.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return nil, ErrUnsupportedContentType
		}
	}

	body, err := ReadBodyLimited(request, limit)
	if err != nil {
		return nil, err
	}
	if !json.Valid(bytes.TrimSpace(body)) {
		return nil, ErrInvalidJSON
	}
	return body, nil
}
