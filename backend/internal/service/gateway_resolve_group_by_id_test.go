//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/ctxkey"
	"github.com/shudonglin/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// gatewayGroupRepoStub provides a minimal GroupRepository for ResolveGroupByID
// hydration tests. It tracks GetByIDLite + GetAccountPlatforms calls and
// returns configurable fixtures.
type gatewayGroupRepoStub struct {
	getByIDLiteGroup     *Group
	getByIDLiteErr       error
	getByIDLiteCalls     int
	accountPlatforms     []string
	accountPlatformsErr  error
	getAccountPlatformsN int
}

func (s *gatewayGroupRepoStub) Create(_ context.Context, _ *Group) error { return nil }
func (s *gatewayGroupRepoStub) GetByID(_ context.Context, _ int64) (*Group, error) {
	panic("unexpected GetByID")
}
func (s *gatewayGroupRepoStub) GetByIDLite(_ context.Context, _ int64) (*Group, error) {
	s.getByIDLiteCalls++
	if s.getByIDLiteErr != nil {
		return nil, s.getByIDLiteErr
	}
	return s.getByIDLiteGroup, nil
}
func (s *gatewayGroupRepoStub) Update(_ context.Context, _ *Group) error { return nil }
func (s *gatewayGroupRepoStub) Delete(_ context.Context, _ int64) error  { return nil }
func (s *gatewayGroupRepoStub) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	return nil, nil
}
func (s *gatewayGroupRepoStub) List(_ context.Context, _ pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *gatewayGroupRepoStub) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _ string, _ *bool) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *gatewayGroupRepoStub) ListActive(_ context.Context) ([]Group, error) {
	return nil, nil
}
func (s *gatewayGroupRepoStub) ListActiveByPlatform(_ context.Context, _ string) ([]Group, error) {
	return nil, nil
}
func (s *gatewayGroupRepoStub) ExistsByName(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *gatewayGroupRepoStub) GetAccountCount(_ context.Context, _ int64) (int64, int64, error) {
	return 0, 0, nil
}
func (s *gatewayGroupRepoStub) GetAccountPlatforms(_ context.Context, _ int64) ([]string, error) {
	s.getAccountPlatformsN++
	if s.accountPlatformsErr != nil {
		return nil, s.accountPlatformsErr
	}
	return s.accountPlatforms, nil
}
func (s *gatewayGroupRepoStub) DeleteAccountGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (s *gatewayGroupRepoStub) GetAccountIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	return nil, nil
}
func (s *gatewayGroupRepoStub) BindAccountsToGroup(_ context.Context, _ int64, _ []int64) error {
	return nil
}
func (s *gatewayGroupRepoStub) UpdateSortOrders(_ context.Context, _ []GroupSortOrderUpdate) error {
	return nil
}

// TestResolveGroupByID_HydratesAccountPlatformsFromRepo verifies that when
// the group is loaded via repo (i.e., not present in context snapshot),
// ResolveGroupByID hydrates AccountPlatforms via groupRepo.GetAccountPlatforms.
// This is the runtime mirror of the admin hydration step (design Change 8d-runtime):
// the runtime fallback validator at gateway_handler.go reads the derived set
// to authorize multi-platform fallback groups.
func TestResolveGroupByID_HydratesAccountPlatformsFromRepo(t *testing.T) {
	repo := &gatewayGroupRepoStub{
		getByIDLiteGroup: &Group{ID: 42, Platform: PlatformOpenAI},
		// Repo reports the group has both OpenAI and Anthropic accounts.
		accountPlatforms: []string{PlatformOpenAI, PlatformAnthropic},
	}
	svc := &GatewayService{groupRepo: repo}

	group, err := svc.ResolveGroupByID(context.Background(), 42)
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, 1, repo.getByIDLiteCalls)
	require.Equal(t, 1, repo.getAccountPlatformsN, "ResolveGroupByID must hydrate AccountPlatforms when not present in snapshot")
	require.Equal(t, []string{PlatformOpenAI, PlatformAnthropic}, group.AccountPlatforms)
}

// TestResolveGroupByID_PreservesPopulatedAccountPlatformsFromContext verifies
// that when the context already supplies a hydrated group (snapshot path),
// ResolveGroupByID does NOT re-query GetAccountPlatforms — preserves snapshot
// fast path and avoids unnecessary DB hits on the hot path.
func TestResolveGroupByID_PreservesPopulatedAccountPlatformsFromContext(t *testing.T) {
	hydrated := &Group{
		ID:               42,
		Platform:         PlatformOpenAI,
		Status:           StatusActive,
		Hydrated:         true,
		AccountPlatforms: []string{PlatformOpenAI, PlatformAnthropic},
	}
	ctx := context.WithValue(context.Background(), ctxkey.Group, hydrated)

	repo := &gatewayGroupRepoStub{} // panics if GetByID is invoked
	svc := &GatewayService{groupRepo: repo}

	group, err := svc.ResolveGroupByID(ctx, 42)
	require.NoError(t, err)
	require.Same(t, hydrated, group, "must reuse the snapshot from context")
	require.Equal(t, 0, repo.getByIDLiteCalls)
	require.Equal(t, 0, repo.getAccountPlatformsN, "context-supplied group must not trigger extra hydration")
}

// TestResolveGroupByID_HydrationErrorLeavesNil verifies that a hydration
// failure falls back to nil AccountPlatforms — readers use the helper which
// then falls back to []string{Platform}, preserving single-platform behavior.
func TestResolveGroupByID_HydrationErrorLeavesNil(t *testing.T) {
	repo := &gatewayGroupRepoStub{
		getByIDLiteGroup:    &Group{ID: 42, Platform: PlatformAnthropic},
		accountPlatformsErr: errors.New("simulated DB error"),
	}
	svc := &GatewayService{groupRepo: repo}

	group, err := svc.ResolveGroupByID(context.Background(), 42)
	require.NoError(t, err, "hydration error must not break the resolve path")
	require.NotNil(t, group)
	require.Nil(t, group.AccountPlatforms, "hydration error leaves AccountPlatforms nil for helper fallback")
	// helper fallback yields the legacy single-platform set
	require.Equal(t, []string{PlatformAnthropic}, GroupAccountPlatforms(group))
}
