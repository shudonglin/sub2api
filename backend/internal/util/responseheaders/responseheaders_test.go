package responseheaders

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestFilterHeadersDisabledUsesDefaultAllowlist(t *testing.T) {
	src := http.Header{}
	src.Add("Content-Type", "application/json")
	src.Add("X-Request-Id", "req-123")
	src.Add("X-Test", "ok")
	src.Add("Connection", "keep-alive")
	src.Add("Content-Length", "123")

	cfg := config.ResponseHeaderConfig{
		Enabled:     false,
		ForceRemove: []string{"x-request-id"},
	}

	filtered := FilterHeaders(src, CompileHeaderFilter(cfg))
	if filtered.Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type passthrough, got %q", filtered.Get("Content-Type"))
	}
	if filtered.Get("X-Request-Id") != "req-123" {
		t.Fatalf("expected X-Request-Id allowed, got %q", filtered.Get("X-Request-Id"))
	}
	if filtered.Get("X-Test") != "" {
		t.Fatalf("expected X-Test removed, got %q", filtered.Get("X-Test"))
	}
	if filtered.Get("Connection") != "" {
		t.Fatalf("expected Connection to be removed, got %q", filtered.Get("Connection"))
	}
	if filtered.Get("Content-Length") != "" {
		t.Fatalf("expected Content-Length to be removed, got %q", filtered.Get("Content-Length"))
	}
}

func TestFilterHeadersEnabledUsesAllowlist(t *testing.T) {
	src := http.Header{}
	src.Add("Content-Type", "application/json")
	src.Add("X-Extra", "ok")
	src.Add("X-Remove", "nope")
	src.Add("X-Blocked", "nope")

	cfg := config.ResponseHeaderConfig{
		Enabled:           true,
		AdditionalAllowed: []string{"x-extra"},
		ForceRemove:       []string{"x-remove"},
	}

	filtered := FilterHeaders(src, CompileHeaderFilter(cfg))
	if filtered.Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type allowed, got %q", filtered.Get("Content-Type"))
	}
	if filtered.Get("X-Extra") != "ok" {
		t.Fatalf("expected X-Extra allowed, got %q", filtered.Get("X-Extra"))
	}
	if filtered.Get("X-Remove") != "" {
		t.Fatalf("expected X-Remove removed, got %q", filtered.Get("X-Remove"))
	}
	if filtered.Get("X-Blocked") != "" {
		t.Fatalf("expected X-Blocked removed, got %q", filtered.Get("X-Blocked"))
	}
}

func TestFilterHeadersBuiltinBlockedAlwaysRemoved(t *testing.T) {
	src := http.Header{}
	src.Add("Set-Cookie", "secret=upstream")
	src.Add("Alt-Svc", "h3=\":443\"")
	src.Add("Server-Timing", "edge;dur=1")
	src.Add("NEL", "{\"report_to\":\"default\"}")
	src.Add("Report-To", "{\"group\":\"default\"}")
	src.Add("Origin-Agent-Cluster", "?1")
	src.Add("Content-Type", "application/json")

	filtered := FilterHeaders(src, CompileHeaderFilter(config.ResponseHeaderConfig{}))
	for _, h := range []string{"Set-Cookie", "Alt-Svc", "Server-Timing", "NEL", "Report-To", "Origin-Agent-Cluster"} {
		if filtered.Get(h) != "" {
			t.Fatalf("expected builtin blocked header %q removed, got %q", h, filtered.Get(h))
		}
	}
	if filtered.Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type preserved, got %q", filtered.Get("Content-Type"))
	}
}

func TestFilterHeadersAdditionalAllowedCannotReenableBuiltinBlocked(t *testing.T) {
	src := http.Header{}
	src.Add("Set-Cookie", "secret=upstream")
	src.Add("Alt-Svc", "h3=\":443\"")
	src.Add("Content-Type", "application/json")

	cfg := config.ResponseHeaderConfig{
		Enabled:           true,
		AdditionalAllowed: []string{"set-cookie", "alt-svc"},
	}
	filtered := FilterHeaders(src, CompileHeaderFilter(cfg))
	if filtered.Get("Set-Cookie") != "" || filtered.Get("Alt-Svc") != "" {
		t.Fatalf("expected builtin blocked headers to stay blocked even with additional_allowed, got Set-Cookie=%q Alt-Svc=%q",
			filtered.Get("Set-Cookie"), filtered.Get("Alt-Svc"))
	}
	if filtered.Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type preserved, got %q", filtered.Get("Content-Type"))
	}
}
