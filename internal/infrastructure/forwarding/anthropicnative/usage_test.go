package anthropicnative

import (
	"bytes"
	"errors"
	"testing"
)

func TestExtractUsageReadsAnthropicNativeUsage(t *testing.T) {
	body := []byte(`{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"content": [{"type":"text","text":"hello"}],
		"model": "claude-3-5-sonnet",
		"usage": {
			"input_tokens": 17,
			"cache_creation_input_tokens": 3,
			"cache_read_input_tokens": 5,
			"output_tokens": 11
		}
	}`)
	original := append([]byte(nil), body...)

	usage, err := ExtractUsage(body)
	if err != nil {
		t.Fatalf("ExtractUsage: %v", err)
	}
	if usage.InputTokens != 17 || usage.OutputTokens != 11 {
		t.Fatalf("usage = %#v", usage)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("body mutated: %s", body)
	}
}

func TestExtractUsageRequiresUsageObjectAndTokenCounts(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "missing usage",
			body: `{"id":"msg_01"}`,
			want: ErrUsageNotFound,
		},
		{
			name: "missing input tokens",
			body: `{"usage":{"output_tokens":1}}`,
			want: ErrUsageNotFound,
		},
		{
			name: "missing output tokens",
			body: `{"usage":{"input_tokens":1}}`,
			want: ErrUsageNotFound,
		},
		{
			name: "malformed json",
			body: `{"usage":`,
			want: ErrInvalidUsage,
		},
		{
			name: "fractional input tokens",
			body: `{"usage":{"input_tokens":1.5,"output_tokens":1}}`,
			want: ErrInvalidUsage,
		},
		{
			name: "negative output tokens",
			body: `{"usage":{"input_tokens":1,"output_tokens":-1}}`,
			want: ErrInvalidUsage,
		},
		{
			name: "string token count",
			body: `{"usage":{"input_tokens":"1","output_tokens":1}}`,
			want: ErrInvalidUsage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ExtractUsage([]byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}
