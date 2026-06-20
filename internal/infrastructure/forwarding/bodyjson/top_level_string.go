package bodyjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var (
	ErrInvalid  = errors.New("invalid top level json string field")
	ErrMismatch = errors.New("top level json string field mismatch")
)

func ReplaceTopLevelString(body []byte, field string, want string, replacement string) ([]byte, error) {
	if strings.TrimSpace(field) == "" || strings.TrimSpace(replacement) == "" {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrInvalid
	}
	seen := false
	start, end := -1, -1
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, ErrInvalid
		}
		valueStart, err := valueStart(body, int(decoder.InputOffset()))
		if err != nil {
			return nil, ErrInvalid
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, ErrInvalid
		}
		valueEnd := valueStart + len(raw)
		if valueEnd > len(body) {
			return nil, ErrInvalid
		}
		if key != field {
			continue
		}
		if seen {
			return nil, ErrInvalid
		}
		seen = true
		start, end = valueStart, valueEnd
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, ErrInvalid
		}
		if strings.TrimSpace(decoded) == "" {
			return nil, ErrInvalid
		}
		if decoded != want {
			return nil, ErrMismatch
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	if !seen {
		return nil, ErrInvalid
	}
	quoted, err := json.Marshal(replacement)
	if err != nil {
		return nil, ErrInvalid
	}
	out := make([]byte, 0, len(body)-(end-start)+len(quoted))
	out = append(out, body[:start]...)
	out = append(out, quoted...)
	out = append(out, body[end:]...)
	return out, nil
}

func valueStart(body []byte, offset int) (int, error) {
	for offset < len(body) && jsonSpace(body[offset]) {
		offset++
	}
	if offset < len(body) && body[offset] == ':' {
		offset++
	}
	for offset < len(body) && jsonSpace(body[offset]) {
		offset++
	}
	if offset >= len(body) {
		return 0, ErrInvalid
	}
	return offset, nil
}

func jsonSpace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
