package openaicompat

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func rewriteTopLevelModel(body []byte, clientModel, providerModel string) ([]byte, error) {
	if strings.TrimSpace(providerModel) == "" {
		return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
	}
	s := jsonScanner{data: body}
	s.skipWS()
	if !s.consume('{') {
		return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
	}
	seenModel := false
	modelStart, modelEnd := -1, -1
	modelValue := ""
	s.skipWS()
	if s.consume('}') {
		s.skipWS()
		if s.pos != len(body) {
			return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
		}
		return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
	}
	for {
		s.skipWS()
		key, err := s.readJSONString()
		if err != nil {
			return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
		}
		s.skipWS()
		if !s.consume(':') {
			return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
		}
		s.skipWS()
		if key == "model" {
			if seenModel {
				return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
			}
			seenModel = true
			modelStart = s.pos
			value, err := s.readJSONString()
			if err != nil {
				return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
			}
			modelEnd = s.pos
			modelValue = value
			if strings.TrimSpace(modelValue) == "" || modelValue != clientModel {
				return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
			}
		} else if err := s.skipValue(); err != nil {
			return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
		}
		s.skipWS()
		if s.consume('}') {
			break
		}
		if !s.consume(',') {
			return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
		}
	}
	s.skipWS()
	if s.pos != len(body) || !seenModel {
		return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
	}
	quoted, err := quoteJSONString(providerModel)
	if err != nil {
		return nil, fmt.Errorf("%w", ErrModelRewriteFailed)
	}
	out := make([]byte, 0, len(body)-(modelEnd-modelStart)+len(quoted))
	out = append(out, body[:modelStart]...)
	out = append(out, quoted...)
	out = append(out, body[modelEnd:]...)
	return out, nil
}

type jsonScanner struct {
	data []byte
	pos  int
}

func (s *jsonScanner) skipWS() {
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case ' ', '\n', '\r', '\t':
			s.pos++
		default:
			return
		}
	}
}

func (s *jsonScanner) consume(b byte) bool {
	if s.pos < len(s.data) && s.data[s.pos] == b {
		s.pos++
		return true
	}
	return false
}

func (s *jsonScanner) readJSONString() (string, error) {
	if !s.consume('"') {
		return "", fmt.Errorf("not string")
	}
	var builder strings.Builder
	chunkStart := s.pos
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		switch {
		case c == '"':
			if err := appendJSONRawStringChunk(&builder, s.data[chunkStart:s.pos]); err != nil {
				return "", err
			}
			s.pos++
			return builder.String(), nil
		case c == '\\':
			if err := appendJSONRawStringChunk(&builder, s.data[chunkStart:s.pos]); err != nil {
				return "", err
			}
			s.pos++
			r, err := s.readJSONEscape()
			if err != nil {
				return "", err
			}
			builder.WriteRune(r)
			chunkStart = s.pos
		case c < 0x20:
			return "", fmt.Errorf("control in string")
		default:
			s.pos++
		}
	}
	return "", fmt.Errorf("unterminated string")
}

func appendJSONRawStringChunk(builder *strings.Builder, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	if !utf8.Valid(chunk) {
		return fmt.Errorf("invalid utf-8 in string")
	}
	builder.Write(chunk)
	return nil
}

func (s *jsonScanner) readJSONEscape() (rune, error) {
	if s.pos >= len(s.data) {
		return 0, fmt.Errorf("unterminated escape")
	}
	c := s.data[s.pos]
	s.pos++
	switch c {
	case '"', '\\', '/':
		return rune(c), nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		first, err := s.readHexQuad()
		if err != nil {
			return 0, err
		}
		if utf16.IsSurrogate(first) {
			if first < 0xD800 || first > 0xDBFF {
				return 0, fmt.Errorf("invalid low surrogate")
			}
			if s.pos+2 > len(s.data) || s.data[s.pos] != '\\' || s.data[s.pos+1] != 'u' {
				return 0, fmt.Errorf("missing low surrogate")
			}
			s.pos += 2
			second, err := s.readHexQuad()
			if err != nil {
				return 0, err
			}
			if second < 0xDC00 || second > 0xDFFF {
				return 0, fmt.Errorf("invalid low surrogate")
			}
			return utf16.DecodeRune(first, second), nil
		}
		return first, nil
	default:
		return 0, fmt.Errorf("invalid escape")
	}
}

func (s *jsonScanner) readHexQuad() (rune, error) {
	if s.pos+4 > len(s.data) {
		return 0, fmt.Errorf("short unicode escape")
	}
	var value rune
	for i := 0; i < 4; i++ {
		nibble, ok := hexValue(s.data[s.pos+i])
		if !ok {
			return 0, fmt.Errorf("invalid unicode escape")
		}
		value = value<<4 | rune(nibble)
	}
	s.pos += 4
	return value, nil
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func quoteJSONString(value string) (string, error) {
	var builder strings.Builder
	builder.Grow(len(value) + 2)
	builder.WriteByte('"')
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			return "", fmt.Errorf("invalid utf-8")
		}
		switch r {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if r < 0x20 {
				builder.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				builder.WriteByte(hex[byte(r)>>4])
				builder.WriteByte(hex[byte(r)&0x0f])
			} else {
				builder.WriteRune(r)
			}
		}
		value = value[size:]
	}
	builder.WriteByte('"')
	return builder.String(), nil
}

func (s *jsonScanner) skipValue() error {
	s.skipWS()
	if s.pos >= len(s.data) {
		return fmt.Errorf("missing value")
	}
	switch s.data[s.pos] {
	case '"':
		_, err := s.readJSONString()
		return err
	case '{':
		return s.skipComposite('{', '}')
	case '[':
		return s.skipComposite('[', ']')
	case 't':
		return s.consumeLiteral("true")
	case 'f':
		return s.consumeLiteral("false")
	case 'n':
		return s.consumeLiteral("null")
	default:
		return s.skipNumber()
	}
}

func (s *jsonScanner) skipComposite(open, close byte) error {
	if !s.consume(open) {
		return fmt.Errorf("missing composite")
	}
	s.skipWS()
	if s.consume(close) {
		return nil
	}
	for {
		if open == '{' {
			s.skipWS()
			if _, err := s.readJSONString(); err != nil {
				return err
			}
			s.skipWS()
			if !s.consume(':') {
				return fmt.Errorf("missing colon")
			}
		}
		if err := s.skipValue(); err != nil {
			return err
		}
		s.skipWS()
		if s.consume(close) {
			return nil
		}
		if !s.consume(',') {
			return fmt.Errorf("missing comma")
		}
	}
}

func (s *jsonScanner) consumeLiteral(lit string) error {
	if !bytes.HasPrefix(s.data[s.pos:], []byte(lit)) {
		return fmt.Errorf("bad literal")
	}
	s.pos += len(lit)
	return nil
}

func (s *jsonScanner) skipNumber() error {
	start := s.pos
	if s.pos < len(s.data) && s.data[s.pos] == '-' {
		s.pos++
	}
	if s.pos >= len(s.data) {
		return fmt.Errorf("bad number")
	}
	if s.data[s.pos] == '0' {
		s.pos++
	} else if s.data[s.pos] >= '1' && s.data[s.pos] <= '9' {
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	} else {
		return fmt.Errorf("bad number")
	}
	if s.pos < len(s.data) && s.data[s.pos] == '.' {
		s.pos++
		if s.pos >= len(s.data) || s.data[s.pos] < '0' || s.data[s.pos] > '9' {
			return fmt.Errorf("bad number")
		}
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	}
	if s.pos < len(s.data) && (s.data[s.pos] == 'e' || s.data[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.data) && (s.data[s.pos] == '+' || s.data[s.pos] == '-') {
			s.pos++
		}
		if s.pos >= len(s.data) || s.data[s.pos] < '0' || s.data[s.pos] > '9' {
			return fmt.Errorf("bad number")
		}
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	}
	if s.pos == start {
		return fmt.Errorf("bad number")
	}
	return nil
}
