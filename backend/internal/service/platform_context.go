// Package service - platform_context provides 3-tier platform resolution helpers
// (ForcePlatform → requested_platform → Group.Platform) for service-layer code that
// reads context.Context. Gin-aware variants live in package middleware.
package service

import (
	"context"

	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
)

// GroupAccountPlatforms returns the derived platform set for a group, with nil-tolerant fallback.
//   - nil group → nil
//   - g.AccountPlatforms != nil (even if empty []) → return as-is (caller queried)
//   - g.AccountPlatforms == nil but g.Platform set → fall back to []string{g.Platform} (legacy snapshot)
//   - both empty → nil
//
// Empty slice (vs nil) is meaningful: it means "I queried and found 0 accounts". Callers must NOT
// treat empty as "use Platform fallback" — that conflates a real zero-account state with stale cache.
func GroupAccountPlatforms(g *Group) []string {
	if g == nil {
		return nil
	}
	if g.AccountPlatforms != nil {
		return g.AccountPlatforms
	}
	if g.Platform != "" {
		return []string{g.Platform}
	}
	return nil
}

// GetRequestedPlatformFromContext reads the router-set platform value from context.Context.
// Returns "" when not set (e.g., direct service-layer calls without router setup, or test helpers).
func GetRequestedPlatformFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxkey.RequestedPlatform).(string); ok {
		return v
	}
	return ""
}

// ResolvePlatformFromContext implements the 3-tier platform resolution chain:
//
//  1. ctx.Value(ctxkey.ForcePlatform) — antigravity / pinned routes (always wins when set)
//  2. ctx.Value(ctxkey.RequestedPlatform) — router-set value for the current URL
//  3. group.Platform — legacy primary-label fallback (preserves single-platform behavior for
//     direct service-layer calls and tests that don't go through router)
//
// Returns "" when all three are empty or nil.
//
// Why ForcePlatform from ctxkey directly: middleware.ForcePlatform writes to BOTH gin.Context AND
// request context.Context (via ctxkey.ForcePlatform). Service code reads context.Context directly,
// not gin, so we use ctxkey here. The middleware-package GetForcePlatformFromContext takes
// *gin.Context, not context.Context, and is unusable from service layer.
func ResolvePlatformFromContext(ctx context.Context, group *Group) string {
	if v, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && v != "" {
		return v
	}
	if rp := GetRequestedPlatformFromContext(ctx); rp != "" {
		return rp
	}
	if group != nil {
		return group.Platform
	}
	return ""
}

// isAttachAllowed reports whether an account with the given platform may be
// attached to a group whose primary platform is groupPlatform. Per the
// multi-platform groups scope guard (design Change 8b): same-platform always
// allowed; openai <-> anthropic cross-platform allowed; everything else
// (including gemini, antigravity) stays rejected.
//
// NOTE: PlatformAzure does not exist as a constant in this codebase; the
// design pseudocode mentioned it as a forward-compatibility hook. The current
// allowlist covers only platforms defined in domain_constants.go.
func isAttachAllowed(groupPlatform, accountPlatform string) bool {
	if groupPlatform == accountPlatform {
		return true
	}
	isOpenAIish := func(p string) bool { return p == PlatformOpenAI }
	isAnthropic := func(p string) bool { return p == PlatformAnthropic }
	if isOpenAIish(groupPlatform) && isAnthropic(accountPlatform) {
		return true
	}
	if isAnthropic(groupPlatform) && isOpenAIish(accountPlatform) {
		return true
	}
	return false
}

// isAccountInScope reports whether an account platform is eligible to
// participate in the multi-platform groups feature scope (per design Change
// 8d-copy: openai/anthropic only). Gemini/antigravity accounts remain
// out-of-scope and must be rejected by copy/attach validators.
func isAccountInScope(p string) bool {
	return p == PlatformOpenAI || p == PlatformAnthropic
}
