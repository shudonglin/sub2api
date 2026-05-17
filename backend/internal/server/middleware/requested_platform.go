package middleware

import (
	"context"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/shudonglin/sub2api/internal/service"
)

// SetRequestedPlatform writes the platform value to BOTH gin.Context (for handler-side reads)
// AND request context.Context (for service-layer reads downstream of c.Request.Context()
// propagation). Both are required: gin.c.Set populates only c.Keys, not the request context;
// service code at e.g. gateway_service.go:2211 reads ctx.Value, not gin.
func SetRequestedPlatform(c *gin.Context, platform string) {
	c.Set(string(ctxkey.RequestedPlatform), platform)
	ctx := context.WithValue(c.Request.Context(), ctxkey.RequestedPlatform, platform)
	c.Request = c.Request.WithContext(ctx)
}

// GetRequestedPlatform reads the requested platform from gin keys first, then request context as
// a defensive fallback. Returns "" when not set (e.g., direct test calls without router setup).
func GetRequestedPlatform(c *gin.Context) string {
	if v, ok := c.Get(string(ctxkey.RequestedPlatform)); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v, ok := c.Request.Context().Value(ctxkey.RequestedPlatform).(string); ok {
		return v
	}
	return ""
}

// GroupHasPlatform reports whether the API key's group derived-platform set contains the given platform.
//
// Short-circuit: if HasForcePlatform(c) is true (antigravity / pinned routes), returns true regardless
// of the group's actual derived set. Antigravity routes pin platform via middleware.ForcePlatform; the
// underlying handler/scheduler enforces account-platform compatibility, not the eligibility check here.
func GroupHasPlatform(c *gin.Context, platform string) bool {
	if HasForcePlatform(c) {
		return true
	}
	apiKey, ok := GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return false
	}
	return slices.Contains(service.GroupAccountPlatforms(apiKey.Group), platform)
}

// ResolvePlatform implements the 3-tier platform resolution chain for handlers reading via gin:
//
//  1. ForcePlatform — antigravity / pinned routes (always wins when set)
//  2. requested_platform — router-set value for the current URL
//  3. apiKey.Group.Platform — legacy primary-label fallback
//
// Returns "" when all three are empty.
//
// Service-layer counterpart with same semantics: service.ResolvePlatformFromContext.
func ResolvePlatform(c *gin.Context, apiKey *service.APIKey) string {
	if forced, ok := GetForcePlatformFromContext(c); ok && forced != "" {
		return forced
	}
	if rp := GetRequestedPlatform(c); rp != "" {
		return rp
	}
	if apiKey != nil && apiKey.Group != nil {
		return apiKey.Group.Platform
	}
	return ""
}
