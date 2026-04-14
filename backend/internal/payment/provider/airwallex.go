// Package provider implements payment provider integrations.
//
// Airwallex provider:
//
// Implements the Hosted Payment Page (HPP) flow against Airwallex's
// PaymentIntent API. The provider authenticates with a short-lived bearer
// token (cached, refreshed 1 minute before upstream expiry), creates a
// PaymentIntent, and returns an HPP redirect URL the user is redirected to.
// Webhook events are verified with HMAC-SHA256 over `timestamp + rawBody`
// against the configured webhook secret.
//
// Required config keys (JSON in payment_provider_instances.config):
//   - clientId       — Airwallex client ID
//   - apiKey         — Airwallex API key
//   - webhookSecret  — secret used to verify webhook HMAC signatures
//   - environment    — "demo" or "prod" (default "prod")
//   - currency       — "USD" | "CNY" | "SGD" (default "USD")
//   - notifyUrl      — webhook destination URL (informational, set in dashboard)
//   - returnUrl      — browser return URL after payment (overridable per request)
package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// Airwallex environments and base URLs.
const (
	airwallexEnvDemo = "demo"
	airwallexEnvProd = "prod"

	airwallexAPIBaseDemo = "https://api-demo.airwallex.com"
	airwallexAPIBaseProd = "https://api.airwallex.com"

	airwallexHPPBaseDemo = "https://checkout-demo.airwallex.com"
	airwallexHPPBaseProd = "https://checkout.airwallex.com"

	// airwallexTokenRefreshSkew refreshes the cached bearer token this much
	// before upstream expiry to avoid edge-case 401s.
	airwallexTokenRefreshSkew = 60 * time.Second

	// airwallexHTTPTimeout caps every Airwallex API HTTP call.
	airwallexHTTPTimeout = 30 * time.Second

	// airwallexEventSucceeded / Failed / Cancelled — webhook event names.
	airwallexEventSucceeded = "payment_intent.succeeded"
	airwallexEventFailed    = "payment_intent.payment_failed"
	airwallexEventCancelled = "payment_intent.cancelled"

	airwallexStatusSucceeded = "SUCCEEDED"
	airwallexStatusCancelled = "CANCELLED"
	airwallexStatusExpired   = "EXPIRED"

	airwallexDefaultCurrency = "USD"
)

// supportedAirwallexCurrencies enumerates currencies accepted by this provider.
// Airwallex itself supports many more, but we restrict to the set the
// downstream order/billing flow understands.
var supportedAirwallexCurrencies = map[string]struct{}{
	"USD": {},
	"CNY": {},
	"SGD": {},
}

// Airwallex implements payment.Provider for Airwallex hosted payments.
type Airwallex struct {
	instanceID string
	config     map[string]string

	httpClient *http.Client

	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewAirwallex constructs an Airwallex provider from a decrypted config map.
func NewAirwallex(instanceID string, config map[string]string) (*Airwallex, error) {
	if config["clientId"] == "" {
		return nil, fmt.Errorf("airwallex config missing required key: clientId")
	}
	if config["apiKey"] == "" {
		return nil, fmt.Errorf("airwallex config missing required key: apiKey")
	}
	if env := config["environment"]; env != "" && env != airwallexEnvDemo && env != airwallexEnvProd {
		return nil, fmt.Errorf("airwallex config invalid environment %q (want %q or %q)", env, airwallexEnvDemo, airwallexEnvProd)
	}
	if cur := strings.ToUpper(strings.TrimSpace(config["currency"])); cur != "" {
		if _, ok := supportedAirwallexCurrencies[cur]; !ok {
			return nil, fmt.Errorf("airwallex config unsupported currency %q (want USD, CNY or SGD)", cur)
		}
	}
	return &Airwallex{
		instanceID: instanceID,
		config:     config,
		httpClient: &http.Client{Timeout: airwallexHTTPTimeout},
	}, nil
}

// Name returns the human-readable provider name.
func (a *Airwallex) Name() string {
	if a.instanceID != "" {
		return "airwallex:" + a.instanceID
	}
	return "Airwallex"
}

// ProviderKey returns the provider key.
func (a *Airwallex) ProviderKey() string { return payment.TypeAirwallex }

// SupportedTypes returns the payment types this provider handles.
func (a *Airwallex) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAirwallex}
}

// environment returns the configured environment, defaulting to prod.
func (a *Airwallex) environment() string {
	if e := a.config["environment"]; e == airwallexEnvDemo {
		return airwallexEnvDemo
	}
	return airwallexEnvProd
}

// currency returns the configured currency, defaulting to USD.
func (a *Airwallex) currency() string {
	if c := strings.ToUpper(strings.TrimSpace(a.config["currency"])); c != "" {
		return c
	}
	return airwallexDefaultCurrency
}

// apiBase returns the Airwallex REST API base URL for the configured environment.
func (a *Airwallex) apiBase() string {
	if a.environment() == airwallexEnvDemo {
		return airwallexAPIBaseDemo
	}
	return airwallexAPIBaseProd
}

// hppBase returns the Airwallex Hosted Payment Page base URL.
func (a *Airwallex) hppBase() string {
	if a.environment() == airwallexEnvDemo {
		return airwallexHPPBaseDemo
	}
	return airwallexHPPBaseProd
}

// ── Auth token cache ─────────────────────────────────────────────────────────

type airwallexAuthResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// getAuthToken returns a valid bearer token, refreshing the cached one if
// it is missing or close to expiry. Safe for concurrent use.
func (a *Airwallex) getAuthToken(ctx context.Context) (string, error) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()

	if a.cachedToken != "" && time.Now().Add(airwallexTokenRefreshSkew).Before(a.tokenExpiry) {
		return a.cachedToken, nil
	}

	reqURL := a.apiBase() + "/api/v1/authentication/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("airwallex auth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", a.config["clientId"])
	req.Header.Set("x-api-key", a.config["apiKey"])

	body, err := a.doHTTP(req)
	if err != nil {
		return "", fmt.Errorf("airwallex auth: %w", err)
	}

	var auth airwallexAuthResponse
	if err := json.Unmarshal(body, &auth); err != nil {
		return "", fmt.Errorf("airwallex auth: decode response: %w", err)
	}
	if auth.Token == "" {
		return "", fmt.Errorf("airwallex auth: empty token in response")
	}

	expiry, err := time.Parse(time.RFC3339, auth.ExpiresAt)
	if err != nil {
		// Some Airwallex responses use RFC3339 with fractional seconds; try a fallback.
		expiry, err = time.Parse("2006-01-02T15:04:05.000Z", auth.ExpiresAt)
		if err != nil {
			// Fall back to a conservative 15-minute window so we don't cache forever.
			expiry = time.Now().Add(15 * time.Minute)
		}
	}

	a.cachedToken = auth.Token
	a.tokenExpiry = expiry
	return a.cachedToken, nil
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

// doHTTP performs the request, reads the body, and converts non-2xx into an
// error containing the upstream payload.
func (a *Airwallex) doHTTP(req *http.Request) ([]byte, error) {
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// apiRequest performs an authenticated JSON request against the Airwallex API
// and unmarshals the response into out (if non-nil).
func (a *Airwallex) apiRequest(ctx context.Context, method, path string, body, out any) error {
	token, err := a.getAuthToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("airwallex %s %s: marshal body: %w", method, path, err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.apiBase()+path, bodyReader)
	if err != nil {
		return fmt.Errorf("airwallex %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	respBody, err := a.doHTTP(req)
	if err != nil {
		return fmt.Errorf("airwallex %s %s: %w", method, path, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("airwallex %s %s: decode response: %w", method, path, err)
	}
	return nil
}

// ── PaymentIntent / Refund payloads ──────────────────────────────────────────

type airwallexPaymentIntent struct {
	ID              string          `json:"id"`
	ClientSecret    string          `json:"client_secret"`
	Status          string          `json:"status"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	MerchantOrderID string          `json:"merchant_order_id,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

type airwallexRefund struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Amount decimal.Decimal `json:"amount"`
}

type airwallexProduct struct {
	Name      string          `json:"name"`
	Quantity  int             `json:"quantity"`
	UnitPrice decimal.Decimal `json:"unit_price"`
}

type airwallexOrder struct {
	Products []airwallexProduct `json:"products"`
}

type airwallexCreateIntentReq struct {
	RequestID       string          `json:"request_id"`
	MerchantOrderID string          `json:"merchant_order_id"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	Order           airwallexOrder  `json:"order"`
	ReturnURL       string          `json:"return_url,omitempty"`
}

type airwallexCreateRefundReq struct {
	RequestID       string          `json:"request_id"`
	PaymentIntentID string          `json:"payment_intent_id"`
	Amount          decimal.Decimal `json:"amount"`
	Reason          string          `json:"reason,omitempty"`
}

// ── HPP URL ──────────────────────────────────────────────────────────────────

// buildHPPURL builds the Hosted Payment Page redirect URL for the given intent.
func (a *Airwallex) buildHPPURL(intentID, clientSecret, currency string) string {
	u, err := url.Parse(a.hppBase() + "/pay")
	if err != nil {
		// hppBase is a constant; this should not happen in practice.
		return ""
	}
	q := u.Query()
	q.Set("intent_id", intentID)
	q.Set("client_secret", clientSecret)
	q.Set("mode", "payment")
	if currency != "" {
		q.Set("currency", currency)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ── Provider interface implementation ────────────────────────────────────────

// CreatePayment creates a PaymentIntent and returns the HPP redirect URL.
func (a *Airwallex) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("airwallex create payment: invalid amount %q: %w", req.Amount, err)
	}
	if amount.Sign() <= 0 {
		return nil, fmt.Errorf("airwallex create payment: amount must be positive, got %q", req.Amount)
	}

	currency := a.currency()
	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = a.config["returnUrl"]
	}

	body := airwallexCreateIntentReq{
		RequestID:       uuid.NewString(),
		MerchantOrderID: req.OrderID,
		Amount:          amount,
		Currency:        currency,
		Metadata:        map[string]any{"orderId": req.OrderID},
		Order: airwallexOrder{
			Products: []airwallexProduct{{
				Name:      req.Subject,
				Quantity:  1,
				UnitPrice: amount,
			}},
		},
		ReturnURL: returnURL,
	}

	var intent airwallexPaymentIntent
	if err := a.apiRequest(ctx, http.MethodPost, "/api/v1/pa/payment_intents/create", body, &intent); err != nil {
		return nil, fmt.Errorf("airwallex create payment: %w", err)
	}

	return &payment.CreatePaymentResponse{
		TradeNo: intent.ID,
		PayURL:  a.buildHPPURL(intent.ID, intent.ClientSecret, currency),
	}, nil
}

// QueryOrder fetches a PaymentIntent and maps Airwallex status to provider status.
func (a *Airwallex) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	if tradeNo == "" {
		return nil, fmt.Errorf("airwallex query order: empty tradeNo")
	}
	var intent airwallexPaymentIntent
	if err := a.apiRequest(ctx, http.MethodGet, "/api/v1/pa/payment_intents/"+url.PathEscape(tradeNo), nil, &intent); err != nil {
		return nil, fmt.Errorf("airwallex query order: %w", err)
	}

	amt, _ := intent.Amount.Float64()
	return &payment.QueryOrderResponse{
		TradeNo: intent.ID,
		Status:  mapAirwallexStatus(intent.Status),
		Amount:  amt,
	}, nil
}

// mapAirwallexStatus maps Airwallex PaymentIntent status to provider status.
func mapAirwallexStatus(s string) string {
	switch s {
	case airwallexStatusSucceeded:
		return payment.ProviderStatusPaid
	case airwallexStatusCancelled, airwallexStatusExpired:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

// ── Webhook verification ─────────────────────────────────────────────────────

// airwallexWebhookEvent is the parsed subset of an Airwallex webhook payload.
type airwallexWebhookEvent struct {
	Name string `json:"name"`
	Data struct {
		Object struct {
			ID              string          `json:"id"`
			MerchantOrderID string          `json:"merchant_order_id"`
			Amount          decimal.Decimal `json:"amount"`
			Status          string          `json:"status"`
			Metadata        map[string]any  `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

// verifyAirwallexSignature returns true if signature is a valid HMAC-SHA256
// of `timestamp + rawBody` keyed with secret.
func verifyAirwallexSignature(rawBody, signature, timestamp, secret string) bool {
	if signature == "" || timestamp == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	// hash.Hash.Write never returns an error (documented contract).
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte(rawBody))
	expected := hex.EncodeToString(mac.Sum(nil))
	// Constant-time comparison over equal-length slices.
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyNotification verifies an Airwallex webhook callback and returns the
// parsed PaymentNotification, or nil for irrelevant event types.
func (a *Airwallex) VerifyNotification(_ context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	secret := a.config["webhookSecret"]
	if secret == "" {
		return nil, fmt.Errorf("airwallex webhookSecret not configured")
	}

	signature := headers["x-signature"]
	timestamp := headers["x-timestamp"]
	if signature == "" || timestamp == "" {
		return nil, fmt.Errorf("airwallex webhook missing x-signature or x-timestamp header")
	}

	if !verifyAirwallexSignature(rawBody, signature, timestamp, secret) {
		return nil, fmt.Errorf("airwallex webhook signature verification failed")
	}

	var event airwallexWebhookEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, fmt.Errorf("airwallex webhook: decode event: %w", err)
	}

	var status string
	switch event.Name {
	case airwallexEventSucceeded:
		status = payment.ProviderStatusSuccess
	case airwallexEventFailed, airwallexEventCancelled:
		status = payment.ProviderStatusFailed
	default:
		// Irrelevant event — caller should ack with 200.
		return nil, nil
	}

	pi := event.Data.Object
	orderID := pi.MerchantOrderID
	if md, ok := pi.Metadata["orderId"].(string); ok && md != "" {
		orderID = md
	}

	amt, _ := pi.Amount.Float64()
	return &payment.PaymentNotification{
		TradeNo: pi.ID,
		OrderID: orderID,
		Amount:  amt,
		Status:  status,
		RawData: rawBody,
	}, nil
}

// Refund creates a refund against the given PaymentIntent.
func (a *Airwallex) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("airwallex refund: invalid amount %q: %w", req.Amount, err)
	}
	if amount.Sign() <= 0 {
		return nil, fmt.Errorf("airwallex refund: amount must be positive, got %q", req.Amount)
	}

	reason := req.Reason
	if reason == "" {
		reason = "Refund requested"
	}

	body := airwallexCreateRefundReq{
		RequestID:       uuid.NewString(),
		PaymentIntentID: req.TradeNo,
		Amount:          amount,
		Reason:          reason,
	}

	var refund airwallexRefund
	if err := a.apiRequest(ctx, http.MethodPost, "/api/v1/pa/refunds/create", body, &refund); err != nil {
		return nil, fmt.Errorf("airwallex refund: %w", err)
	}

	status := payment.ProviderStatusPending
	if refund.Status == airwallexStatusSucceeded {
		status = payment.ProviderStatusSuccess
	}
	return &payment.RefundResponse{
		RefundID: refund.ID,
		Status:   status,
	}, nil
}

// Ensure interface compliance.
var _ payment.Provider = (*Airwallex)(nil)
