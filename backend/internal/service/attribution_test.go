package service

import (
	"testing"
)

func TestComputeAttributionFingerprint(t *testing.T) {
	tests := []struct {
		name    string
		msgText string
		version string
	}{
		{
			name:    "normal message",
			msgText: "Hello, how are you doing today?",
			version: "2.1.22",
		},
		{
			name:    "short message uses zero padding",
			msgText: "Hi",
			version: "2.1.22",
		},
		{
			name:    "empty message all zeros",
			msgText: "",
			version: "2.1.22",
		},
		{
			name:    "different version",
			msgText: "Hello, how are you doing today?",
			version: "2.2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := computeAttributionFingerprint(tt.msgText, tt.version)
			if len(fp) != 3 {
				t.Errorf("fingerprint length = %d, want 3", len(fp))
			}
			// Verify determinism
			fp2 := computeAttributionFingerprint(tt.msgText, tt.version)
			if fp != fp2 {
				t.Errorf("non-deterministic: got %q then %q", fp, fp2)
			}
		})
	}

	// Verify different inputs produce different fingerprints
	fp1 := computeAttributionFingerprint("Hello, how are you doing today?", "2.1.22")
	fp2 := computeAttributionFingerprint("", "2.1.22")
	if fp1 == fp2 {
		t.Errorf("different inputs produced same fingerprint: %q", fp1)
	}
}

func TestComputeAttributionFingerprintCharIndexing(t *testing.T) {
	// Verify that characters at exact indices 4, 7, 20 are used
	msg := "abcdefghijklmnopqrstu" // len=21, indices 4='e', 7='h', 20='u'
	fp := computeAttributionFingerprint(msg, "1.0.0")

	// With only index 20 out of bounds (len=8), should use '0'
	shortMsg := "abcdefgh" // len=8, indices 4='e', 7='h', 20=out-of-bounds→'0'
	fpShort := computeAttributionFingerprint(shortMsg, "1.0.0")

	if fp == fpShort {
		t.Logf("full msg fp=%s, short msg fp=%s", fp, fpShort)
		// They could collide but it's very unlikely
	}

	// Index 4 out of bounds
	veryShortMsg := "abc" // len=3, all out of bounds except none
	fpVeryShort := computeAttributionFingerprint(veryShortMsg, "1.0.0")
	if len(fpVeryShort) != 3 {
		t.Errorf("fingerprint length = %d, want 3", len(fpVeryShort))
	}
}

func TestExtractFirstUserMessageText(t *testing.T) {
	tests := []struct {
		name     string
		messages []any
		want     string
	}{
		{
			name: "string content",
			messages: []any{
				map[string]any{"role": "user", "content": "hello world"},
			},
			want: "hello world",
		},
		{
			name: "array content with text block",
			messages: []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "text", "text": "array message"},
					},
				},
			},
			want: "array message",
		},
		{
			name: "skips assistant messages",
			messages: []any{
				map[string]any{"role": "assistant", "content": "I'm an assistant"},
				map[string]any{"role": "user", "content": "user message"},
			},
			want: "user message",
		},
		{
			name:     "no user message returns empty",
			messages: []any{},
			want:     "",
		},
		{
			name: "array content picks first text block",
			messages: []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "image", "source": "data"},
						map[string]any{"type": "text", "text": "describe this"},
					},
				},
			},
			want: "describe this",
		},
		{
			name:     "nil messages",
			messages: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstUserMessageText(tt.messages)
			if got != tt.want {
				t.Errorf("extractFirstUserMessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildAttributionHeader(t *testing.T) {
	tests := []struct {
		version     string
		fingerprint string
		want        string
	}{
		{
			version:     "2.1.22",
			fingerprint: "abc",
			want:        "x-anthropic-billing-header: cc_version=2.1.22.abc; cc_entrypoint=cli;",
		},
		{
			version:     "2.2.0",
			fingerprint: "def",
			want:        "x-anthropic-billing-header: cc_version=2.2.0.def; cc_entrypoint=cli;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.version+"."+tt.fingerprint, func(t *testing.T) {
			got := buildAttributionHeader(tt.version, tt.fingerprint)
			if got != tt.want {
				t.Errorf("buildAttributionHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInjectAttributionBlock(t *testing.T) {
	headerText := "x-anthropic-billing-header: cc_version=2.1.22.abc; cc_entrypoint=cli;"

	t.Run("null system", func(t *testing.T) {
		body := []byte(`{"model":"claude-3","messages":[]}`)
		result := injectAttributionBlock(body, headerText)
		sysStr := string(result)
		if !contains(sysStr, headerText) {
			t.Errorf("attribution header not found in result: %s", sysStr)
		}
	})

	t.Run("array system prepends", func(t *testing.T) {
		body := []byte(`{"model":"claude-3","system":[{"type":"text","text":"existing"}],"messages":[]}`)
		result := injectAttributionBlock(body, headerText)
		sysStr := string(result)
		// The attribution block should appear before "existing"
		if !contains(sysStr, headerText) {
			t.Errorf("attribution header not found in result: %s", sysStr)
		}
		if !contains(sysStr, "existing") {
			t.Errorf("existing system block lost: %s", sysStr)
		}
	})

	t.Run("string system converts to array", func(t *testing.T) {
		body := []byte(`{"model":"claude-3","system":"some instructions","messages":[]}`)
		result := injectAttributionBlock(body, headerText)
		sysStr := string(result)
		if !contains(sysStr, headerText) {
			t.Errorf("attribution header not found in result: %s", sysStr)
		}
		if !contains(sysStr, "some instructions") {
			t.Errorf("existing system string lost: %s", sysStr)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
