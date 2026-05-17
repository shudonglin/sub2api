package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/shudonglin/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	return c
}

func TestSetRequestedPlatform_WritesBothGinAndRequestContext(t *testing.T) {
	c := newTestContext(t)
	SetRequestedPlatform(c, "anthropic")

	// gin keys
	require.Equal(t, "anthropic", c.GetString(string(ctxkey.RequestedPlatform)))
	// request context
	v, ok := c.Request.Context().Value(ctxkey.RequestedPlatform).(string)
	require.True(t, ok)
	require.Equal(t, "anthropic", v)
}

func TestGetRequestedPlatform_ReadsFromGinFirst(t *testing.T) {
	c := newTestContext(t)
	SetRequestedPlatform(c, "anthropic")
	require.Equal(t, "anthropic", GetRequestedPlatform(c))
}

func TestGetRequestedPlatform_FallsBackToRequestContext(t *testing.T) {
	c := newTestContext(t)
	// Only set request context, not gin keys.
	ctx := context.WithValue(c.Request.Context(), ctxkey.RequestedPlatform, "openai")
	c.Request = c.Request.WithContext(ctx)
	require.Equal(t, "openai", GetRequestedPlatform(c))
}

func TestGetRequestedPlatform_EmptyByDefault(t *testing.T) {
	c := newTestContext(t)
	require.Equal(t, "", GetRequestedPlatform(c))
}

func TestGroupHasPlatform_ForcePlatformShortCircuit(t *testing.T) {
	c := newTestContext(t)
	// Antigravity middleware sets ForcePlatform; group has no antigravity account.
	c.Set(string(ContextKeyForcePlatform), "antigravity")
	apiKey := &service.APIKey{Group: &service.Group{Platform: "anthropic", AccountPlatforms: []string{"anthropic"}}}
	c.Set(string(ContextKeyAPIKey), apiKey)
	require.True(t, GroupHasPlatform(c, "antigravity"), "ForcePlatform short-circuit should return true regardless of derived set")
}

func TestGroupHasPlatform_DerivedSet_Hit(t *testing.T) {
	c := newTestContext(t)
	apiKey := &service.APIKey{Group: &service.Group{Platform: "openai", AccountPlatforms: []string{"openai", "anthropic"}}}
	c.Set(string(ContextKeyAPIKey), apiKey)
	require.True(t, GroupHasPlatform(c, "anthropic"))
	require.True(t, GroupHasPlatform(c, "openai"))
	require.False(t, GroupHasPlatform(c, "gemini"))
}

func TestGroupHasPlatform_NilField_LegacyPlatformFallback(t *testing.T) {
	c := newTestContext(t)
	// AccountPlatforms nil → service.GroupAccountPlatforms falls back to [Platform]
	apiKey := &service.APIKey{Group: &service.Group{Platform: "openai"}}
	c.Set(string(ContextKeyAPIKey), apiKey)
	require.True(t, GroupHasPlatform(c, "openai"))
	require.False(t, GroupHasPlatform(c, "anthropic"))
}

func TestGroupHasPlatform_NoAPIKey(t *testing.T) {
	c := newTestContext(t)
	require.False(t, GroupHasPlatform(c, "openai"))
}

func TestGroupHasPlatform_NilGroup(t *testing.T) {
	c := newTestContext(t)
	apiKey := &service.APIKey{Group: nil}
	c.Set(string(ContextKeyAPIKey), apiKey)
	require.False(t, GroupHasPlatform(c, "openai"))
}

func TestResolvePlatform_ForcePlatformWins(t *testing.T) {
	c := newTestContext(t)
	c.Set(string(ContextKeyForcePlatform), "antigravity")
	SetRequestedPlatform(c, "anthropic")                                 // ignored
	apiKey := &service.APIKey{Group: &service.Group{Platform: "openai"}} // ignored
	require.Equal(t, "antigravity", ResolvePlatform(c, apiKey))
}

func TestResolvePlatform_RequestedPlatformOverGroup(t *testing.T) {
	c := newTestContext(t)
	SetRequestedPlatform(c, "anthropic")
	apiKey := &service.APIKey{Group: &service.Group{Platform: "openai"}}
	require.Equal(t, "anthropic", ResolvePlatform(c, apiKey))
}

func TestResolvePlatform_FallsBackToGroupPlatform(t *testing.T) {
	c := newTestContext(t)
	apiKey := &service.APIKey{Group: &service.Group{Platform: "openai"}}
	require.Equal(t, "openai", ResolvePlatform(c, apiKey))
}

func TestResolvePlatform_NilAPIKey(t *testing.T) {
	c := newTestContext(t)
	require.Equal(t, "", ResolvePlatform(c, nil))
}
