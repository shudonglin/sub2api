//go:build unit

// Tests for the copy-account scope guard introduced in Phase 7 (design
// Change 8d-copy): replace the per-group platform-equality rejection with
// a per-account scope check. Accounts whose platform is in {openai,
// anthropic} may be copied across primary-platform boundaries, subject to
// the isAttachAllowed allowlist (openai <-> anthropic). Gemini and
// antigravity accounts remain rejected (out of scope).

package service

import (
	"context"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// groupRepoStubForCopyAccounts implements GroupRepository for copy-validator
// tests. Source groups are looked up by ID; account IDs and BindAccounts
// invocations are recorded.
type groupRepoStubForCopyAccounts struct {
	groups               map[int64]*Group
	created              *Group
	updated              *Group
	accountIDsByGroupIDs []int64
	getAccountIDsErr     error
	bindGroupID          int64
	bindAccountIDs       []int64
	bindCalled           bool
	accountPlatforms     map[int64][]string
}

func (s *groupRepoStubForCopyAccounts) Create(_ context.Context, g *Group) error {
	if g.ID == 0 {
		g.ID = 999
	}
	s.created = g
	if s.groups == nil {
		s.groups = map[int64]*Group{}
	}
	s.groups[g.ID] = g
	return nil
}

func (s *groupRepoStubForCopyAccounts) Update(_ context.Context, g *Group) error {
	s.updated = g
	return nil
}

func (s *groupRepoStubForCopyAccounts) GetByID(ctx context.Context, id int64) (*Group, error) {
	return s.GetByIDLite(ctx, id)
}

func (s *groupRepoStubForCopyAccounts) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	if g, ok := s.groups[id]; ok {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

func (s *groupRepoStubForCopyAccounts) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete call")
}
func (s *groupRepoStubForCopyAccounts) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (s *groupRepoStubForCopyAccounts) List(_ context.Context, _ pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *groupRepoStubForCopyAccounts) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _ string, _ *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (s *groupRepoStubForCopyAccounts) ListActive(_ context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (s *groupRepoStubForCopyAccounts) ListActiveByPlatform(_ context.Context, _ string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (s *groupRepoStubForCopyAccounts) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (s *groupRepoStubForCopyAccounts) GetAccountCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}

func (s *groupRepoStubForCopyAccounts) GetAccountPlatforms(_ context.Context, id int64) ([]string, error) {
	if s.accountPlatforms != nil {
		if p, ok := s.accountPlatforms[id]; ok {
			return p, nil
		}
	}
	return nil, nil
}

func (s *groupRepoStubForCopyAccounts) DeleteAccountGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (s *groupRepoStubForCopyAccounts) GetAccountIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	if s.getAccountIDsErr != nil {
		return nil, s.getAccountIDsErr
	}
	return s.accountIDsByGroupIDs, nil
}

func (s *groupRepoStubForCopyAccounts) BindAccountsToGroup(_ context.Context, groupID int64, accountIDs []int64) error {
	s.bindCalled = true
	s.bindGroupID = groupID
	s.bindAccountIDs = append([]int64{}, accountIDs...)
	return nil
}

func (s *groupRepoStubForCopyAccounts) UpdateSortOrders(_ context.Context, _ []GroupSortOrderUpdate) error {
	return nil
}

// accountRepoStubForCopyAccounts implements AccountRepository for copy
// validator tests. Only the methods actually invoked are non-panicking.
type accountRepoStubForCopyAccounts struct {
	accountRepoStub
	accountsByID map[int64]*Account
}

func (s *accountRepoStubForCopyAccounts) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if acc, ok := s.accountsByID[id]; ok {
			out = append(out, acc)
		}
	}
	return out, nil
}

// TestCopyAccounts_OpenAIIntoAnthropicGroup_CreateGroup_Allowed verifies that
// CreateGroup with CopyAccountsFromGroupIDs permits copying OpenAI accounts
// into an anthropic-primary group (per design Change 8d-copy: per-account
// scope check replaces per-group platform-equality rejection).
func TestCopyAccounts_OpenAIIntoAnthropicGroup_CreateGroup_Allowed(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{100, 200},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			100: {ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			200: {ID: 200, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "anthropic-primary-with-openai",
		Platform:                 PlatformAnthropic,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.NoError(t, err, "openai accounts must be copy-allowed into an anthropic-primary group")
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.True(t, repo.bindCalled, "BindAccountsToGroup must be called")
	require.ElementsMatch(t, []int64{100, 200}, repo.bindAccountIDs)
}

// TestCopyAccounts_AnthropicIntoOpenAIGroup_CreateGroup_Allowed verifies the
// reverse direction.
func TestCopyAccounts_AnthropicIntoOpenAIGroup_CreateGroup_Allowed(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformAnthropic, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{300},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			300: {ID: 300, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "openai-primary-with-anthropic",
		Platform:                 PlatformOpenAI,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.NoError(t, err)
	require.NotNil(t, group)
	require.True(t, repo.bindCalled)
	require.ElementsMatch(t, []int64{300}, repo.bindAccountIDs)
}

// TestCopyAccounts_GeminiIntoOpenAIGroup_CreateGroup_Rejected verifies that
// gemini accounts (out-of-scope) are still rejected by the per-account scope
// check.
func TestCopyAccounts_GeminiIntoOpenAIGroup_CreateGroup_Rejected(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformGemini, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{400},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			400: {ID: 400, Platform: PlatformGemini, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "openai-primary",
		Platform:                 PlatformOpenAI,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.Error(t, err, "gemini accounts must remain out-of-scope for the multi-platform mix")
	require.False(t, repo.bindCalled, "BindAccountsToGroup must not be called when scope check fails")
}

// TestCopyAccounts_AntigravityIntoAnthropicGroup_CreateGroup_Rejected
// verifies that antigravity accounts (out-of-scope) are rejected.
func TestCopyAccounts_AntigravityIntoAnthropicGroup_CreateGroup_Rejected(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformAntigravity, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{500},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			500: {ID: 500, Platform: PlatformAntigravity, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "anthropic-primary",
		Platform:                 PlatformAnthropic,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.Error(t, err, "antigravity accounts must stay rejected per scope")
	require.False(t, repo.bindCalled)
}

// TestCopyAccounts_SamePlatform_CreateGroup_Allowed regression: copying
// accounts within the same primary platform must continue to work
// identically.
func TestCopyAccounts_SamePlatform_CreateGroup_Allowed(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{100},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			100: {ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "openai-primary",
		Platform:                 PlatformOpenAI,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.NoError(t, err)
	require.True(t, repo.bindCalled)
}

// TestCopyAccounts_SameAntigravityPlatform_CreateGroup_Allowed regression:
// same-platform antigravity copy must keep working (out-of-scope crosses
// stay rejected, same-platform crosses do not).
func TestCopyAccounts_SameAntigravityPlatform_CreateGroup_Allowed(t *testing.T) {
	srcGroupID := int64(1)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformAntigravity, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{600},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			600: {ID: 600, Platform: PlatformAntigravity, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "antigravity-only",
		Platform:                 PlatformAntigravity,
		RateMultiplier:           1.0,
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.NoError(t, err)
	require.True(t, repo.bindCalled)
}

// TestCopyAccounts_OpenAIIntoAnthropicGroup_UpdateGroup_Allowed mirrors the
// CreateGroup test for the UpdateGroup copy path.
func TestCopyAccounts_OpenAIIntoAnthropicGroup_UpdateGroup_Allowed(t *testing.T) {
	srcGroupID := int64(1)
	targetID := int64(50)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformOpenAI, Status: StatusActive},
			targetID:   {ID: targetID, Platform: PlatformAnthropic, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{100, 200},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			100: {ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			200: {ID: 200, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.UpdateGroup(context.Background(), targetID, &UpdateGroupInput{
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.NoError(t, err, "openai accounts must be copy-allowed into anthropic-primary group")
	require.True(t, repo.bindCalled)
	require.Equal(t, targetID, repo.bindGroupID)
	require.ElementsMatch(t, []int64{100, 200}, repo.bindAccountIDs)
}

// TestCopyAccounts_GeminiIntoOpenAIGroup_UpdateGroup_Rejected mirrors the
// CreateGroup test for UpdateGroup.
func TestCopyAccounts_GeminiIntoOpenAIGroup_UpdateGroup_Rejected(t *testing.T) {
	srcGroupID := int64(1)
	targetID := int64(50)
	repo := &groupRepoStubForCopyAccounts{
		groups: map[int64]*Group{
			srcGroupID: {ID: srcGroupID, Platform: PlatformGemini, Status: StatusActive},
			targetID:   {ID: targetID, Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountIDsByGroupIDs: []int64{400},
	}
	acctRepo := &accountRepoStubForCopyAccounts{
		accountsByID: map[int64]*Account{
			400: {ID: 400, Platform: PlatformGemini, Type: AccountTypeOAuth},
		},
	}
	svc := &adminServiceImpl{groupRepo: repo, accountRepo: acctRepo}

	_, err := svc.UpdateGroup(context.Background(), targetID, &UpdateGroupInput{
		CopyAccountsFromGroupIDs: []int64{srcGroupID},
	})
	require.Error(t, err, "gemini accounts stay out-of-scope")
	require.False(t, repo.bindCalled)
}
