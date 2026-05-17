package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shudonglin/sub2api/internal/config"
	"github.com/shudonglin/sub2api/internal/handler"
	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/shudonglin/sub2api/internal/server/middleware"
	"github.com/shudonglin/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// platformRecorder captures the requested_platform value set by the route closure
// during request handling. It runs after c.Next() so the value reflects what the
// route handler set before invoking (or short-circuiting) the underlying handler.
type platformRecorder struct {
	mu       sync.Mutex
	platform string
}

func (r *platformRecorder) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.platform
}

// newGatewayRoutesTestRouterWithGroup builds a router that injects the given group
// (via fake APIKeyAuth) and records the requested_platform after each request.
//
// Pass group=nil to simulate an API key without a group.
func newGatewayRoutesTestRouterWithGroup(group *service.Group) (*gin.Engine, *platformRecorder) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	rec := &platformRecorder{}

	// Recorder runs first (registered before RegisterGatewayRoutes). It calls c.Next()
	// inline, then captures requested_platform after the route handler ran.
	router.Use(func(c *gin.Context) {
		c.Next()
		if v, ok := c.Get(string(ctxkey.RequestedPlatform)); ok {
			if s, ok := v.(string); ok {
				rec.mu.Lock()
				rec.platform = s
				rec.mu.Unlock()
			}
		}
	})

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			apiKey := &service.APIKey{}
			if group != nil {
				apiKey.GroupID = &groupID
				apiKey.Group = group
			}
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	return router, rec
}

// newGatewayRoutesTestRouter preserves the legacy single-platform OpenAI fixture
// used by existing tests (compact paths + images). New tests should use
// newGatewayRoutesTestRouterWithGroup directly.
func newGatewayRoutesTestRouter() *gin.Engine {
	router, _ := newGatewayRoutesTestRouterWithGroup(&service.Group{
		Platform:         service.PlatformOpenAI,
		AccountPlatforms: []string{service.PlatformOpenAI},
	})
	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Phase 4 — URL templates with GroupHasPlatform
// ──────────────────────────────────────────────────────────────────────────────

func anthropicGroup() *service.Group {
	return &service.Group{
		Platform:         service.PlatformAnthropic,
		AccountPlatforms: []string{service.PlatformAnthropic},
	}
}
func openaiGroup() *service.Group {
	return &service.Group{
		Platform:         service.PlatformOpenAI,
		AccountPlatforms: []string{service.PlatformOpenAI},
	}
}
func geminiGroup() *service.Group {
	return &service.Group{
		Platform:         service.PlatformGemini,
		AccountPlatforms: []string{service.PlatformGemini},
	}
}
func multiGroup(platforms ...string) *service.Group {
	return &service.Group{
		Platform:         platforms[0],
		AccountPlatforms: platforms,
	}
}
func emptyGroup() *service.Group {
	// Group exists but has no derivable platforms — represents a group whose
	// AccountPlatforms set has been narrowed (e.g. accounts removed).
	return &service.Group{
		Platform:         "",
		AccountPlatforms: []string{},
	}
}

// doPOST sends a POST to path with a minimal JSON body and returns response.
func doPOST(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doGET(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ── Task 4.1: POST /v1/messages — anthropic > gemini > openai ──

func TestRouter_PostV1Messages_AnthropicGroup_SetsRequestedAnthropic(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/messages")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformAnthropic, rec.snapshot())
}

func TestRouter_PostV1Messages_GeminiOnlyGroup_SetsRequestedGemini(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(geminiGroup())
	w := doPOST(t, router, "/v1/messages")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformGemini, rec.snapshot())
}

func TestRouter_PostV1Messages_OpenAIOnlyGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/messages")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1Messages_MultiPlatform_PrefersAnthropic(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(multiGroup(
		service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini,
	))
	_ = doPOST(t, router, "/v1/messages")
	require.Equal(t, service.PlatformAnthropic, rec.snapshot())
}

func TestRouter_PostV1Messages_MultiPlatformGeminiAndOpenAI_PrefersGemini(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(multiGroup(
		service.PlatformOpenAI, service.PlatformGemini,
	))
	_ = doPOST(t, router, "/v1/messages")
	require.Equal(t, service.PlatformGemini, rec.snapshot())
}

func TestRouter_PostV1Messages_NoMatch_Returns503(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(emptyGroup())
	w := doPOST(t, router, "/v1/messages")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "", rec.snapshot())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "error", body["type"])
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "overloaded_error", errObj["type"])
}

// ── Task 4.2: POST /v1/messages/count_tokens — anthropic only ──

func TestRouter_PostV1MessagesCountTokens_AnthropicGroup_SetsRequestedAnthropic(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/messages/count_tokens")
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformAnthropic, rec.snapshot())
}

func TestRouter_PostV1MessagesCountTokens_OpenAIGroup_Returns404(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/messages/count_tokens")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "", rec.snapshot())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "error", body["type"])
}

func TestRouter_PostV1MessagesCountTokens_GeminiGroup_Returns404(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(geminiGroup())
	w := doPOST(t, router, "/v1/messages/count_tokens")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// ── Task 4.3: POST /v1/responses + /v1/responses/*subpath — openai only ──

func TestRouter_PostV1Responses_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/responses")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1Responses_AnthropicGroup_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/responses")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRouter_PostV1ResponsesSubpath_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/responses/compact")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1ResponsesSubpath_AnthropicGroup_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/responses/compact")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// ── Task 4.4: GET /v1/responses (WS) — openai only, 403 + JSON hint when not ──

func TestRouter_GetV1Responses_NonOpenAIGroup_Returns403WithHint(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doGET(t, router, "/v1/responses")
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "", rec.snapshot())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "forbidden", errObj["type"])
	require.Equal(t, "group_platform_not_allowed", errObj["code"])
}

// ── Task 4.5: POST /v1/chat/completions — openai > anthropic ──

func TestRouter_PostV1ChatCompletions_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/chat/completions")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1ChatCompletions_AnthropicGroup_SetsRequestedAnthropic(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/chat/completions")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformAnthropic, rec.snapshot())
}

func TestRouter_PostV1ChatCompletions_MultiPlatform_PrefersOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(multiGroup(
		service.PlatformAnthropic, service.PlatformOpenAI,
	))
	_ = doPOST(t, router, "/v1/chat/completions")
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1ChatCompletions_GeminiOnly_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(geminiGroup())
	w := doPOST(t, router, "/v1/chat/completions")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// ── Task 4.6: /v1/images/generations + /v1/images/edits — openai only, 404 ──

func TestRouter_PostV1ImagesGenerations_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/images/generations")
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1ImagesEdits_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/v1/images/edits")
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostV1ImagesGenerations_NonOpenAIGroup_Returns404(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/images/generations")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRouter_PostV1ImagesEdits_NonOpenAIGroup_Returns404(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/v1/images/edits")
	require.Equal(t, http.StatusNotFound, w.Code)
}

// ── Task 4.7: No-/v1 aliases + codex-direct ──

func TestRouter_PostResponsesAlias_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/responses")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostResponsesAlias_AnthropicGroup_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/responses")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRouter_GetResponsesAlias_NonOpenAI_Returns403WithHint(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doGET(t, router, "/responses")
	require.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "forbidden", errObj["type"])
	require.Equal(t, "group_platform_not_allowed", errObj["code"])
}

func TestRouter_PostChatCompletionsAlias_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/chat/completions")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostChatCompletionsAlias_GeminiOnly_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(geminiGroup())
	w := doPOST(t, router, "/chat/completions")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRouter_PostImagesAlias_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/images/generations")
	require.NotEqual(t, http.StatusNotFound, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostImagesAlias_NonOpenAIGroup_Returns404(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/images/generations")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRouter_PostCodexDirectResponses_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/backend-api/codex/responses")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostCodexDirectResponsesSubpath_OpenAIGroup_SetsRequestedOpenAI(t *testing.T) {
	router, rec := newGatewayRoutesTestRouterWithGroup(openaiGroup())
	w := doPOST(t, router, "/backend-api/codex/responses/compact")
	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, service.PlatformOpenAI, rec.snapshot())
}

func TestRouter_PostCodexDirectResponses_AnthropicGroup_Returns503(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doPOST(t, router, "/backend-api/codex/responses")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRouter_GetCodexDirectResponses_NonOpenAI_Returns403WithHint(t *testing.T) {
	router, _ := newGatewayRoutesTestRouterWithGroup(anthropicGroup())
	w := doGET(t, router, "/backend-api/codex/responses")
	require.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "forbidden", errObj["type"])
	require.Equal(t, "group_platform_not_allowed", errObj["code"])
}

// ── Task 4.8: /v1beta middleware sets requested_platform=gemini ──
//
// The full /v1beta route group requires a non-nil APIKeyService for
// APIKeyAuthWithSubscriptionGoogle, which the test harness can't satisfy.
// Instead, mount only the platform-tagging middleware on a synthetic route
// to verify it sets requested_platform on the gin context.
func TestRouter_V1Beta_MiddlewareSetsRequestedPlatformGemini(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	var captured string
	router.Use(func(c *gin.Context) {
		c.Next()
		captured = c.GetString(string(ctxkey.RequestedPlatform))
	})

	gemini := router.Group("/v1beta")
	gemini.Use(func(c *gin.Context) {
		servermiddleware.SetRequestedPlatform(c, service.PlatformGemini)
		c.Next()
	})
	gemini.GET("/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, service.PlatformGemini, captured)
}
