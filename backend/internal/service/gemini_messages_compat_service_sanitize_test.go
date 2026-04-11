package service

import (
	"strings"
	"testing"
)

func TestSanitizeUpstreamErrorMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string // all must appear in output
		wantAbsent  []string // none must appear in output
		wantExact   string   // if non-empty, output must equal this exactly
	}{
		{
			name:      "empty input returns empty",
			input:     "",
			wantExact: "",
		},
		{
			name:        "plain message without secrets unchanged",
			input:       "upstream returned 429 too many requests",
			wantContain: []string{"upstream returned 429 too many requests"},
		},
		{
			name:        "existing query param masking preserved",
			input:       "https://api.example.com/v1?key=supersecretkey&other=value",
			wantAbsent:  []string{"supersecretkey"},
			wantContain: []string{"key=***"},
		},
		{
			name:        "absolute Unix file path stripped",
			input:       "error at /home/user/project/service/handler.go:123:45",
			wantAbsent:  []string{"/home/user/project/service/handler.go:123:45"},
			wantContain: []string{"<path>"},
		},
		{
			name:        "absolute Windows file path stripped",
			input:       `error in C:\Users\dev\app\main.go:42`,
			wantAbsent:  []string{`C:\Users\dev\app\main.go:42`},
			wantContain: []string{"<path>"},
		},
		{
			name:        "internal RFC-1918 IP class A stripped",
			input:       "connection refused to 10.0.1.25:8080",
			wantAbsent:  []string{"10.0.1.25:8080"},
			wantContain: []string{"<internal>"},
		},
		{
			name:        "internal RFC-1918 IP class C stripped",
			input:       "dial tcp 192.168.1.100:5432: connection refused",
			wantAbsent:  []string{"192.168.1.100:5432"},
			wantContain: []string{"<internal>"},
		},
		{
			name:        "internal RFC-1918 IP class B stripped",
			input:       "timeout reaching 172.16.0.5",
			wantAbsent:  []string{"172.16.0.5"},
			wantContain: []string{"<internal>"},
		},
		{
			name:        "public IP not stripped",
			input:       "request to 8.8.8.8:443 failed",
			wantContain: []string{"8.8.8.8:443"},
		},
		{
			name:        "tab-indented stack frame dropped",
			input:       "goroutine panicked\n\truntime/debug.Stack()\n\tsomefile.go:99",
			wantAbsent:  []string{"\truntime/debug.Stack()", "\tsomefile.go:99"},
			wantContain: []string{"goroutine panicked"},
		},
		{
			name:        "bearer token masked with prefix preserved",
			input:       "Authorization: Bearer eyJhbGciOiJSUzI1NiJ9abcdefghijk failed",
			wantContain: []string{"Bearer "},
			wantAbsent:  []string{"eyJhbGciOiJSUzI1NiJ9abcdefghijk"},
		},
		{
			name:        "sk- key masked",
			input:       "invalid key sk-abcdefgh12345678901234 for request",
			wantAbsent:  []string{"sk-abcdefgh12345678901234"},
			wantContain: []string{"***"},
		},
		{
			name:        "long hex blob masked",
			input:       "signing secret abcdef1234567890abcdef1234567890 not valid",
			wantAbsent:  []string{"abcdef1234567890abcdef1234567890"},
			wantContain: []string{"***"},
		},
		{
			name:  "length cap at 500 chars with truncation marker",
			input: strings.Repeat("x", 600),
			// Output must be exactly 500 + len("...[truncated]") chars.
			wantContain: []string{"...[truncated]"},
		},
		{
			name:  "length at exactly 500 chars not truncated",
			input: strings.Repeat("a", 500),
			// No truncation marker expected.
			wantAbsent: []string{"...[truncated]"},
		},
		{
			name: "combined payload — all patterns fire",
			input: "error: sk-abcdefgh12345678901234 auth failed at /app/handler.go:10 " +
				"connecting to 192.168.1.50:5432 ?key=topsecret",
			wantAbsent: []string{
				"sk-abcdefgh12345678901234",
				"/app/handler.go:10",
				"192.168.1.50:5432",
				"topsecret",
			},
			wantContain: []string{"<path>", "<internal>", "key=***"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUpstreamErrorMessage(tt.input)

			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("sanitizeUpstreamErrorMessage(%q) = %q, want exact %q", tt.input, got, tt.wantExact)
				}
				return
			}

			for _, sub := range tt.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("output %q should contain %q", got, sub)
				}
			}
			for _, sub := range tt.wantAbsent {
				if strings.Contains(got, sub) {
					t.Errorf("output %q should NOT contain %q", got, sub)
				}
			}

			// Length cap check.
			maxAllowed := sanitizeMaxLen + len("...[truncated]")
			if len(got) > maxAllowed {
				t.Errorf("output length %d exceeds cap %d", len(got), maxAllowed)
			}
		})
	}
}

func TestMaskSanitizeToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "***"},
		{"short", "***"},
		{"abcdefghijkl", "abcd***kl"},             // 12 chars
		{"abcdefghijklmnopqrs", "abcd***rs"},      // 19 chars
		{"abcdefghijklmnopqrst", "abcdef***qrst"}, // 20 chars
	}
	for _, tt := range tests {
		got := maskSanitizeToken(tt.input)
		if got != tt.want {
			t.Errorf("maskSanitizeToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
