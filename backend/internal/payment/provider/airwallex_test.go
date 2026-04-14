//go:build unit

package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestNewAirwallex(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"clientId": "client",
		"apiKey":   "key",
	}
	clone := func(over map[string]string) map[string]string {
		out := make(map[string]string, len(base))
		for k, v := range base {
			out[k] = v
		}
		for k, v := range over {
			out[k] = v
		}
		return out
	}

	tests := []struct {
		name      string
		config    map[string]string
		wantErr   bool
		errSubstr string
	}{
		{name: "valid", config: base, wantErr: false},
		{name: "missing clientId", config: clone(map[string]string{"clientId": ""}), wantErr: true, errSubstr: "clientId"},
		{name: "missing apiKey", config: clone(map[string]string{"apiKey": ""}), wantErr: true, errSubstr: "apiKey"},
		{name: "valid env demo", config: clone(map[string]string{"environment": "demo"}), wantErr: false},
		{name: "valid env prod", config: clone(map[string]string{"environment": "prod"}), wantErr: false},
		{name: "invalid env", config: clone(map[string]string{"environment": "staging"}), wantErr: true, errSubstr: "environment"},
		{name: "valid currency USD", config: clone(map[string]string{"currency": "USD"}), wantErr: false},
		{name: "valid currency cny lowercase", config: clone(map[string]string{"currency": "cny"}), wantErr: false},
		{name: "invalid currency", config: clone(map[string]string{"currency": "EUR"}), wantErr: true, errSubstr: "currency"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewAirwallex("inst-1", tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil provider")
			}
			if got.ProviderKey() != payment.TypeAirwallex {
				t.Errorf("ProviderKey = %q, want %q", got.ProviderKey(), payment.TypeAirwallex)
			}
			if !strings.Contains(got.Name(), "inst-1") {
				t.Errorf("Name() = %q, want to include instance id", got.Name())
			}
		})
	}
}

func TestAirwallexDefaults(t *testing.T) {
	t.Parallel()
	a, err := NewAirwallex("", map[string]string{"clientId": "c", "apiKey": "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.environment() != airwallexEnvProd {
		t.Errorf("default environment = %q, want %q", a.environment(), airwallexEnvProd)
	}
	if a.currency() != airwallexDefaultCurrency {
		t.Errorf("default currency = %q, want %q", a.currency(), airwallexDefaultCurrency)
	}
	if a.apiBase() != airwallexAPIBaseProd {
		t.Errorf("default apiBase = %q, want %q", a.apiBase(), airwallexAPIBaseProd)
	}

	demo, err := NewAirwallex("", map[string]string{"clientId": "c", "apiKey": "k", "environment": "demo", "currency": "sgd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if demo.apiBase() != airwallexAPIBaseDemo {
		t.Errorf("demo apiBase = %q, want %q", demo.apiBase(), airwallexAPIBaseDemo)
	}
	if demo.currency() != "SGD" {
		t.Errorf("currency uppercased = %q, want SGD", demo.currency())
	}
	if !strings.Contains(demo.Name(), "Airwallex") && !strings.Contains(demo.Name(), "airwallex") {
		t.Errorf("Name() = %q", demo.Name())
	}
}

func TestVerifyAirwallexSignature(t *testing.T) {
	t.Parallel()

	secret := "supersecret"
	body := `{"name":"payment_intent.succeeded","data":{"object":{"id":"int_1"}}}`
	timestamp := "1700000000"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(body))
	good := hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name string
		sig  string
		ts   string
		sec  string
		want bool
	}{
		{"valid", good, timestamp, secret, true},
		{"wrong secret", good, timestamp, "other", false},
		{"wrong timestamp", good, "1700000001", secret, false},
		{"empty signature", "", timestamp, secret, false},
		{"empty timestamp", good, "", secret, false},
		{"empty secret", good, timestamp, "", false},
		{"tampered signature", good[:len(good)-1] + "0", timestamp, secret, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := verifyAirwallexSignature(body, tt.sig, tt.ts, tt.sec)
			if got != tt.want {
				t.Errorf("verifyAirwallexSignature got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestAirwallexVerifyNotification(t *testing.T) {
	t.Parallel()

	secret := "whsec"
	a, err := NewAirwallex("", map[string]string{
		"clientId":      "c",
		"apiKey":        "k",
		"webhookSecret": secret,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := `{"name":"payment_intent.succeeded","data":{"object":{"id":"int_42","merchant_order_id":"order_x","amount":12.34,"status":"SUCCEEDED","metadata":{"orderId":"order_x"}}}}`
	ts := "1700001234"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))

	t.Run("valid succeeded event", func(t *testing.T) {
		t.Parallel()
		notif, err := a.VerifyNotification(context.Background(), body, map[string]string{
			"x-signature": sig,
			"x-timestamp": ts,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notif == nil {
			t.Fatal("expected notification, got nil")
		}
		if notif.TradeNo != "int_42" {
			t.Errorf("TradeNo = %q", notif.TradeNo)
		}
		if notif.OrderID != "order_x" {
			t.Errorf("OrderID = %q", notif.OrderID)
		}
		if notif.Status != payment.ProviderStatusSuccess {
			t.Errorf("Status = %q", notif.Status)
		}
		if notif.Amount != 12.34 {
			t.Errorf("Amount = %v", notif.Amount)
		}
	})

	t.Run("missing webhook secret", func(t *testing.T) {
		t.Parallel()
		noSecret, _ := NewAirwallex("", map[string]string{"clientId": "c", "apiKey": "k"})
		_, err := noSecret.VerifyNotification(context.Background(), body, map[string]string{"x-signature": sig, "x-timestamp": ts})
		if err == nil {
			t.Fatal("expected error for missing webhook secret")
		}
	})

	t.Run("missing headers", func(t *testing.T) {
		t.Parallel()
		_, err := a.VerifyNotification(context.Background(), body, map[string]string{})
		if err == nil {
			t.Fatal("expected error for missing headers")
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		t.Parallel()
		_, err := a.VerifyNotification(context.Background(), body, map[string]string{
			"x-signature": strings.Repeat("0", len(sig)),
			"x-timestamp": ts,
		})
		if err == nil {
			t.Fatal("expected error for bad signature")
		}
	})

	t.Run("irrelevant event returns nil", func(t *testing.T) {
		t.Parallel()
		other := `{"name":"payment_intent.created","data":{"object":{"id":"int_99"}}}`
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts))
		mac.Write([]byte(other))
		s := hex.EncodeToString(mac.Sum(nil))
		notif, err := a.VerifyNotification(context.Background(), other, map[string]string{
			"x-signature": s,
			"x-timestamp": ts,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notif != nil {
			t.Errorf("expected nil notification for irrelevant event")
		}
	})

	t.Run("failed event maps to failed", func(t *testing.T) {
		t.Parallel()
		failedBody := `{"name":"payment_intent.payment_failed","data":{"object":{"id":"int_5","amount":10,"status":"FAILED"}}}`
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts))
		mac.Write([]byte(failedBody))
		s := hex.EncodeToString(mac.Sum(nil))
		notif, err := a.VerifyNotification(context.Background(), failedBody, map[string]string{
			"x-signature": s,
			"x-timestamp": ts,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if notif == nil || notif.Status != payment.ProviderStatusFailed {
			t.Errorf("got %+v, want failed status", notif)
		}
	})
}

func TestMapAirwallexLinkStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status    string
		paidCount int
		want      string
	}{
		{"UNPAID", 0, payment.ProviderStatusPending},
		{"UNPAID", 1, payment.ProviderStatusPaid},
		{"PAID", 0, payment.ProviderStatusPaid},
		{"EXPIRED", 0, payment.ProviderStatusFailed},
		{"INACTIVE", 0, payment.ProviderStatusFailed},
		{"", 0, payment.ProviderStatusPending},
	}
	for _, c := range cases {
		if got := mapAirwallexLinkStatus(c.status, c.paidCount); got != c.want {
			t.Errorf("mapAirwallexLinkStatus(%q, %d) = %q, want %q", c.status, c.paidCount, got, c.want)
		}
	}
}

// TestAirwallexTokenCache verifies the auth token is cached across calls and
// refreshed only after expiry minus skew.
func TestAirwallexTokenCache(t *testing.T) {
	t.Parallel()

	var loginCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/authentication/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		// Issue a token valid for 10 minutes (well beyond the skew).
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      "tok-abc",
			"expires_at": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a, err := NewAirwallex("", map[string]string{"clientId": "c", "apiKey": "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Override the API base by stubbing httpClient via a custom transport.
	// Simpler: directly request login through getAuthToken with a manual base.
	// We test the cache contract by calling the unexported helper with a
	// rewritten apiBase using a temp config trick — but config is read in the
	// helper. Instead, drive through a wrapped client: replace httpClient with
	// one that rewrites the request URL to the test server.
	a.httpClient = &http.Client{
		Transport: rewriteTransport{base: srv.URL, inner: http.DefaultTransport},
		Timeout:   5 * time.Second,
	}

	tok1, err := a.getAuthToken(context.Background())
	if err != nil {
		t.Fatalf("getAuthToken: %v", err)
	}
	tok2, err := a.getAuthToken(context.Background())
	if err != nil {
		t.Fatalf("getAuthToken second call: %v", err)
	}
	if tok1 != "tok-abc" || tok2 != "tok-abc" {
		t.Errorf("unexpected tokens: %q %q", tok1, tok2)
	}
	if loginCalls != 1 {
		t.Errorf("expected 1 login call (cache hit on second), got %d", loginCalls)
	}

	// Force expiry within the skew window — should trigger a refresh.
	a.tokenMu.Lock()
	a.tokenExpiry = time.Now().Add(10 * time.Second) // < refresh skew (60s)
	a.tokenMu.Unlock()

	if _, err := a.getAuthToken(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if loginCalls != 2 {
		t.Errorf("expected refresh to call login again, total=%d", loginCalls)
	}
}

// rewriteTransport rewrites every outgoing request to point at base, preserving
// the path + query. Used in tests to redirect Airwallex SDK calls to httptest.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, err := url.Parse(rt.base)
	if err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = u.Scheme
	r2.URL.Host = u.Host
	r2.Host = u.Host
	return rt.inner.RoundTrip(r2)
}
