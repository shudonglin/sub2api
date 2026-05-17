//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// Phase 5: focused tests for GeminiMessagesCompatService.resolvePlatformAndSchedulingMode
// 3-tier resolution. The helper now reads ResolvePlatformFromContext(ctx, group), so:
//   - Single-platform gemini groups (no requested_platform) fall back to group.Platform — identical
//     to pre-Phase-5 behavior; useMixedScheduling stays true for gemini.
//   - requested_platform=anthropic on multi-platform group → resolved=anthropic; useMixedScheduling=false
//     because mixed scheduling here is gated on resolved == PlatformGemini.
//   - ForcePlatform is handled by the outer branch (preserves hasForcePlatform=true short-circuit).

func TestGeminiCompat_ResolvePlatform_SinglePlatformFallback(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	groupRepo := &mockGroupRepoForGemini{
		groups: map[int64]*Group{
			groupID: {ID: groupID, Platform: PlatformGemini},
		},
	}
	svc := &GeminiMessagesCompatService{groupRepo: groupRepo}

	platform, useMixed, hasForce, err := svc.resolvePlatformAndSchedulingMode(ctx, &groupID)
	require.NoError(t, err)
	require.False(t, hasForce)
	require.Equal(t, PlatformGemini, platform, "single-platform gemini group should resolve to gemini")
	require.True(t, useMixed, "gemini groups enable mixed scheduling")
}

func TestGeminiCompat_ResolvePlatform_RequestedPlatformAnthropic(t *testing.T) {
	groupID := int64(7)
	groupRepo := &mockGroupRepoForGemini{
		groups: map[int64]*Group{
			groupID: {ID: groupID, Platform: PlatformGemini, AccountPlatforms: []string{PlatformGemini, PlatformAnthropic}},
		},
	}
	svc := &GeminiMessagesCompatService{groupRepo: groupRepo}
	ctx := context.WithValue(context.Background(), ctxkey.RequestedPlatform, PlatformAnthropic)

	platform, useMixed, hasForce, err := svc.resolvePlatformAndSchedulingMode(ctx, &groupID)
	require.NoError(t, err)
	require.False(t, hasForce)
	require.Equal(t, PlatformAnthropic, platform, "requested_platform=anthropic should override group.Platform")
	require.False(t, useMixed, "anthropic-resolved path should not enable gemini-only mixed scheduling")
}

func TestGeminiCompat_ResolvePlatform_ForcePlatformWins(t *testing.T) {
	groupID := int64(7)
	groupRepo := &mockGroupRepoForGemini{
		groups: map[int64]*Group{
			groupID: {ID: groupID, Platform: PlatformGemini},
		},
	}
	svc := &GeminiMessagesCompatService{groupRepo: groupRepo}
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity)
	ctx = context.WithValue(ctx, ctxkey.RequestedPlatform, PlatformAnthropic) // ignored

	platform, useMixed, hasForce, err := svc.resolvePlatformAndSchedulingMode(ctx, &groupID)
	require.NoError(t, err)
	require.True(t, hasForce, "ForcePlatform must set hasForcePlatform=true")
	require.Equal(t, PlatformAntigravity, platform)
	require.False(t, useMixed, "force platform path must not enable mixed scheduling")
}
