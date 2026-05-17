//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// Phase 5: focused tests for GatewayService.resolvePlatform 3-tier resolution.
//
// The helper is the critical scheduling decision site updated in Phase 5. These tests verify:
//   - Single-platform groups (no requested_platform set, AccountPlatforms nil) fall back to
//     Group.Platform — byte-level identical to pre-Phase-5 behavior.
//   - Multi-platform groups with requested_platform=anthropic in context resolve to anthropic.
//   - ForcePlatform wins over requested_platform and Group.Platform.

func TestGatewayService_ResolvePlatform_SinglePlatformFallback(t *testing.T) {
	svc := &GatewayService{}
	group := &Group{Platform: PlatformAnthropic}
	platform, hasForce, err := svc.resolvePlatform(context.Background(), nil, group)
	require.NoError(t, err)
	require.False(t, hasForce, "no ForcePlatform → hasForcePlatform must be false (mixed scheduling allowed)")
	require.Equal(t, PlatformAnthropic, platform)
}

func TestGatewayService_ResolvePlatform_RequestedPlatformOverridesGroup(t *testing.T) {
	svc := &GatewayService{}
	group := &Group{Platform: PlatformOpenAI, AccountPlatforms: []string{PlatformOpenAI, PlatformAnthropic}}
	ctx := context.WithValue(context.Background(), ctxkey.RequestedPlatform, PlatformAnthropic)
	platform, hasForce, err := svc.resolvePlatform(ctx, nil, group)
	require.NoError(t, err)
	require.False(t, hasForce, "requested_platform path must not set hasForcePlatform (mixed scheduling stays enabled)")
	require.Equal(t, PlatformAnthropic, platform)
}

func TestGatewayService_ResolvePlatform_ForcePlatformWins(t *testing.T) {
	svc := &GatewayService{}
	group := &Group{Platform: PlatformOpenAI, AccountPlatforms: []string{PlatformOpenAI, PlatformAnthropic}}
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity)
	ctx = context.WithValue(ctx, ctxkey.RequestedPlatform, PlatformAnthropic) // ignored
	platform, hasForce, err := svc.resolvePlatform(ctx, nil, group)
	require.NoError(t, err)
	require.True(t, hasForce, "ForcePlatform must set hasForcePlatform=true to disable mixed scheduling")
	require.Equal(t, PlatformAntigravity, platform)
}

func TestGatewayService_ResolvePlatform_NilGroupNoIDFallsBackToAnthropic(t *testing.T) {
	svc := &GatewayService{}
	platform, hasForce, err := svc.resolvePlatform(context.Background(), nil, nil)
	require.NoError(t, err)
	require.False(t, hasForce)
	require.Equal(t, PlatformAnthropic, platform, "no group, no groupID, no force → legacy default = anthropic")
}
