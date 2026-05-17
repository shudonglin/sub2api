package service

import (
	"context"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGroupAccountPlatforms_NilGroup(t *testing.T) {
	require.Nil(t, GroupAccountPlatforms(nil))
}

func TestGroupAccountPlatforms_NilField_FallsBackToPlatform(t *testing.T) {
	g := &Group{Platform: "openai"} // AccountPlatforms == nil
	require.Equal(t, []string{"openai"}, GroupAccountPlatforms(g))
}

func TestGroupAccountPlatforms_EmptySlice_NotFallback(t *testing.T) {
	g := &Group{Platform: "openai", AccountPlatforms: []string{}} // queried, 0 accounts
	out := GroupAccountPlatforms(g)
	require.NotNil(t, out)
	require.Empty(t, out)
}

func TestGroupAccountPlatforms_PopulatedField(t *testing.T) {
	g := &Group{Platform: "openai", AccountPlatforms: []string{"openai", "anthropic"}}
	require.Equal(t, []string{"openai", "anthropic"}, GroupAccountPlatforms(g))
}

func TestGroupAccountPlatforms_BothEmpty(t *testing.T) {
	g := &Group{} // Platform == "", AccountPlatforms == nil
	require.Nil(t, GroupAccountPlatforms(g))
}

func TestGetRequestedPlatformFromContext_Empty(t *testing.T) {
	require.Equal(t, "", GetRequestedPlatformFromContext(context.Background()))
}

func TestGetRequestedPlatformFromContext_Populated(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.RequestedPlatform, "anthropic")
	require.Equal(t, "anthropic", GetRequestedPlatformFromContext(ctx))
}

func TestResolvePlatformFromContext_ForcePlatformWins(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, "antigravity")
	ctx = context.WithValue(ctx, ctxkey.RequestedPlatform, "anthropic") // should be ignored
	g := &Group{Platform: "openai"}                                     // should be ignored
	require.Equal(t, "antigravity", ResolvePlatformFromContext(ctx, g))
}

func TestResolvePlatformFromContext_RequestedPlatformOverGroup(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.RequestedPlatform, "anthropic")
	g := &Group{Platform: "openai"}
	require.Equal(t, "anthropic", ResolvePlatformFromContext(ctx, g))
}

func TestResolvePlatformFromContext_FallsBackToGroupPlatform(t *testing.T) {
	g := &Group{Platform: "openai"}
	require.Equal(t, "openai", ResolvePlatformFromContext(context.Background(), g))
}

func TestResolvePlatformFromContext_NilGroup_EmptyContext(t *testing.T) {
	require.Equal(t, "", ResolvePlatformFromContext(context.Background(), nil))
}

func TestResolvePlatformFromContext_EmptyForcePlatform_FallsThrough(t *testing.T) {
	// Empty-string ForcePlatform should not block tier 2/3.
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, "")
	ctx = context.WithValue(ctx, ctxkey.RequestedPlatform, "anthropic")
	g := &Group{Platform: "openai"}
	require.Equal(t, "anthropic", ResolvePlatformFromContext(ctx, g))
}

// TestIsAttachAllowed verifies the per-scope attach allowlist used by admin
// account-attach + copy validators. Per design Change 8b: feature targets
// openai+anthropic mix only; gemini/antigravity cross-platform attach stays
// rejected. Same-platform always passes.
func TestIsAttachAllowed(t *testing.T) {
	cases := []struct {
		name            string
		groupPlatform   string
		accountPlatform string
		want            bool
	}{
		// Same-platform: trivially allowed
		{"openai_to_openai", PlatformOpenAI, PlatformOpenAI, true},
		{"anthropic_to_anthropic", PlatformAnthropic, PlatformAnthropic, true},
		{"gemini_to_gemini", PlatformGemini, PlatformGemini, true},
		{"antigravity_to_antigravity", PlatformAntigravity, PlatformAntigravity, true},

		// In-scope cross-platform: openai <-> anthropic
		{"openai_group_anthropic_account", PlatformOpenAI, PlatformAnthropic, true},
		{"anthropic_group_openai_account", PlatformAnthropic, PlatformOpenAI, true},

		// Out-of-scope cross-platform: gemini / antigravity stay rejected
		{"openai_group_gemini_account", PlatformOpenAI, PlatformGemini, false},
		{"openai_group_antigravity_account", PlatformOpenAI, PlatformAntigravity, false},
		{"anthropic_group_gemini_account", PlatformAnthropic, PlatformGemini, false},
		{"anthropic_group_antigravity_account", PlatformAnthropic, PlatformAntigravity, false},
		{"gemini_group_openai_account", PlatformGemini, PlatformOpenAI, false},
		{"gemini_group_anthropic_account", PlatformGemini, PlatformAnthropic, false},
		{"antigravity_group_openai_account", PlatformAntigravity, PlatformOpenAI, false},
		{"antigravity_group_anthropic_account", PlatformAntigravity, PlatformAnthropic, false},
		{"gemini_group_antigravity_account", PlatformGemini, PlatformAntigravity, false},
		{"antigravity_group_gemini_account", PlatformAntigravity, PlatformGemini, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isAttachAllowed(tc.groupPlatform, tc.accountPlatform))
		})
	}
}

// TestIsAccountInScope verifies the per-scope account eligibility check used
// by copy-account validators. Only openai/anthropic accounts are in-scope for
// the multi-platform mix; gemini/antigravity accounts remain ineligible.
func TestIsAccountInScope(t *testing.T) {
	require.True(t, isAccountInScope(PlatformOpenAI))
	require.True(t, isAccountInScope(PlatformAnthropic))
	require.False(t, isAccountInScope(PlatformGemini))
	require.False(t, isAccountInScope(PlatformAntigravity))
	require.False(t, isAccountInScope(""))
	require.False(t, isAccountInScope("unknown"))
}
