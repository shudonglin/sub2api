package routes

import (
	"fmt"
	"net/http"

	"github.com/shudonglin/sub2api/internal/config"
	"github.com/shudonglin/sub2api/internal/handler"
	"github.com/shudonglin/sub2api/internal/server/middleware"
	"github.com/shudonglin/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	// ── Per-URL handlers (closure-shared between /v1 group and no-/v1 alias routes) ──

	// POST /messages — anthropic > gemini > openai; 503 on no match
	messagesHandler := func(c *gin.Context) {
		switch {
		case middleware.GroupHasPlatform(c, service.PlatformAnthropic):
			middleware.SetRequestedPlatform(c, service.PlatformAnthropic)
			h.Gateway.Messages(c)
		case middleware.GroupHasPlatform(c, service.PlatformGemini):
			middleware.SetRequestedPlatform(c, service.PlatformGemini)
			h.Gateway.Messages(c) // Gateway.Messages handles internal Gemini compat branch
		case middleware.GroupHasPlatform(c, service.PlatformOpenAI):
			middleware.SetRequestedPlatform(c, service.PlatformOpenAI)
			h.OpenAIGateway.Messages(c) // legacy translation path (messages_dispatch)
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "overloaded_error",
					"message": "Group has no accounts able to serve /v1/messages (expected anthropic, gemini, or openai).",
				},
			})
		}
	}

	// POST /messages/count_tokens — anthropic only; 404 otherwise
	countTokensHandler := func(c *gin.Context) {
		if middleware.GroupHasPlatform(c, service.PlatformAnthropic) {
			middleware.SetRequestedPlatform(c, service.PlatformAnthropic)
			h.Gateway.CountTokens(c)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Token counting requires anthropic accounts in this group.",
			},
		})
	}

	// POST /responses + /responses/*subpath — openai only; 503 otherwise
	responsesHandler := func(c *gin.Context) {
		if middleware.GroupHasPlatform(c, service.PlatformOpenAI) {
			middleware.SetRequestedPlatform(c, service.PlatformOpenAI)
			h.OpenAIGateway.Responses(c)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "server_error",
				"code":    "no_available_accounts",
				"message": "Responses API requires openai accounts in this group.",
			},
		})
	}

	// GET /responses (WebSocket upgrade) — openai only; 403 + JSON hint otherwise
	responsesWSHandler := func(c *gin.Context) {
		if !middleware.GroupHasPlatform(c, service.PlatformOpenAI) {
			// Include the group's actual derived platforms in the hint per spec D3
			// (helps the client understand WHY openai isn't available).
			var groupPlatforms []string
			if apiKey, ok := middleware.GetAPIKeyFromContext(c); ok && apiKey.Group != nil {
				groupPlatforms = service.GroupAccountPlatforms(apiKey.Group)
			}
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"type": "forbidden",
					"code": "group_platform_not_allowed",
					"message": fmt.Sprintf(
						"WebSocket /v1/responses requires the group to allow openai. Current group platforms: %v.",
						groupPlatforms,
					),
				},
			})
			c.Abort()
			return
		}
		middleware.SetRequestedPlatform(c, service.PlatformOpenAI)
		h.OpenAIGateway.ResponsesWebSocket(c)
	}

	// POST /chat/completions — openai > anthropic; 503 on no match
	chatCompletionsHandler := func(c *gin.Context) {
		switch {
		case middleware.GroupHasPlatform(c, service.PlatformOpenAI):
			middleware.SetRequestedPlatform(c, service.PlatformOpenAI)
			h.OpenAIGateway.ChatCompletions(c)
		case middleware.GroupHasPlatform(c, service.PlatformAnthropic):
			middleware.SetRequestedPlatform(c, service.PlatformAnthropic)
			h.Gateway.ChatCompletions(c) // legacy translation
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"type":    "server_error",
					"code":    "no_available_accounts",
					"message": "Chat Completions requires openai or anthropic accounts in this group.",
				},
			})
		}
	}

	// POST /images/generations + /images/edits — openai only; 404 otherwise
	imagesHandler := func(c *gin.Context) {
		if !middleware.GroupHasPlatform(c, service.PlatformOpenAI) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		middleware.SetRequestedPlatform(c, service.PlatformOpenAI)
		h.OpenAIGateway.Images(c)
	}

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.Use(requireGroupAnthropic)
	{
		gateway.POST("/messages", messagesHandler)
		gateway.POST("/messages/count_tokens", countTokensHandler)
		gateway.GET("/models", h.Gateway.Models)
		gateway.GET("/usage", h.Gateway.Usage)
		gateway.POST("/responses", responsesHandler)
		gateway.POST("/responses/*subpath", responsesHandler)
		gateway.GET("/responses", responsesWSHandler)
		gateway.POST("/chat/completions", chatCompletionsHandler)
		gateway.POST("/images/generations", imagesHandler)
		gateway.POST("/images/edits", imagesHandler)
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(requireGroupGoogle)
	gemini.Use(func(c *gin.Context) {
		middleware.SetRequestedPlatform(c, service.PlatformGemini)
		c.Next()
	})
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// 不带 /v1 前缀的别名路由（responses / chat-completions / images）
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesWSHandler)
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic)
	{
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", responsesHandler)
		codexDirect.GET("/responses", responsesWSHandler)
	}
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, chatCompletionsHandler)
	r.POST("/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, imagesHandler)
	r.POST("/images/edits", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, imagesHandler)

	// Antigravity 模型列表
	r.GET("/antigravity/models", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}
