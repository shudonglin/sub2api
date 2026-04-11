package logger

import (
	"testing"
)

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "***",
		},
		{
			name:  "short string under 12 chars",
			input: "abc123",
			want:  "***",
		},
		{
			name:  "exactly 11 chars (boundary below 12)",
			input: "abcdefghijk",
			want:  "***",
		},
		{
			name:  "exactly 12 chars (keep first4 + last2)",
			input: "abcdefghijkl",
			want:  "abcd***kl",
		},
		{
			name:  "19 chars (keep first4 + last2)",
			input: "abcdefghijklmnopqrs",
			want:  "abcd***rs",
		},
		{
			name:  "21 chars (keep first6 + last4)",
			input: "sk-abcdef1234567890xy",
			want:  "sk-abc***90xy",
		},
		{
			name:  "long sk- key",
			input: "sk-abcdef1234567890xyz",
			want:  "sk-abc***0xyz",
		},
		{
			name:  "30 char string",
			input: "abcdefghijklmnopqrstuvwxyz1234",
			want:  "abcdef***1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskToken(tt.input)
			if got != tt.want {
				t.Errorf("MaskToken(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string — no match, pass through",
			input: "",
			want:  "",
		},
		{
			name:  "plain non-key string — unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "sk- prefixed key long enough",
			input: "sk-abcdefgh12345678",
			want:  MaskToken("sk-abcdefgh12345678"),
		},
		{
			name:  "cr_ prefixed key",
			input: "cr_abcdefgh12345678",
			want:  MaskToken("cr_abcdefgh12345678"),
		},
		{
			name:  "pk_ prefixed key",
			input: "pk_abcdefgh12345678",
			want:  MaskToken("pk_abcdefgh12345678"),
		},
		{
			name:  "Bearer prefixed token",
			input: "Bearer abcdefgh12345678",
			want:  MaskToken("Bearer abcdefgh12345678"),
		},
		{
			name:  "sk- prefix but suffix too short (< 8 chars) — unchanged",
			input: "sk-abc",
			want:  "sk-abc",
		},
		{
			name:  "random email — unchanged",
			input: "user@example.com",
			want:  "user@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskAPIKey(tt.input)
			if got != tt.want {
				t.Errorf("MaskAPIKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
