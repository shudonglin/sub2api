//go:build unit

// 验证 AdminService 在 CreateAccount/UpdateAccount/BulkUpdateAccounts 三个写路径
// 修改 account-group 绑定后，会失效 API Key 认证缓存（按 group ID）。
// 这是 multi-platform-groups Phase 3 的核心保证：cached AccountPlatforms 必须
// 在 TTL 内随成员变化同步刷新。

package service

import (
	"context"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// accountRepoStubForCreateAndBind 在 accountRepoStubForBulkUpdate 之上叠加 Create 行为，
// 用于 CreateAccount 测试避开 panic。
type accountRepoStubForCreateAndBind struct {
	accountRepoStubForBulkUpdate
	createErr error
	created   *Account
}

func (s *accountRepoStubForCreateAndBind) Create(_ context.Context, account *Account) error {
	if s.createErr != nil {
		return s.createErr
	}
	if account.ID == 0 {
		account.ID = 1234
	}
	s.created = account
	return nil
}

func (s *accountRepoStubForCreateAndBind) Update(_ context.Context, _ *Account) error {
	return nil
}

// groupRepoStubForAdminInvalidate 满足 GroupRepository，且支持 ExistsByIDs（批量校验快路径）。
type groupRepoStubForAdminInvalidate struct {
	groupRepoStubForAdmin
	getByIDPlatform string
}

func (s *groupRepoStubForAdminInvalidate) ExistsByIDs(_ context.Context, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func (s *groupRepoStubForAdminInvalidate) GetByID(_ context.Context, id int64) (*Group, error) {
	platform := s.getByIDPlatform
	if platform == "" {
		platform = PlatformAnthropic
	}
	return &Group{ID: id, Platform: platform}, nil
}

// TestAdminService_CreateAccount_InvalidatesAuthCacheForBoundGroups 验证创建账号后绑定的所有分组都被失效。
func TestAdminService_CreateAccount_InvalidatesAuthCacheForBoundGroups(t *testing.T) {
	repo := &accountRepoStubForCreateAndBind{}
	groupRepo := &groupRepoStubForAdminInvalidate{}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		accountRepo:          repo,
		groupRepo:            groupRepo,
		authCacheInvalidator: invalidator,
	}

	input := &CreateAccountInput{
		Name:                  "acct-create",
		Platform:              PlatformAnthropic,
		Type:                  AccountTypeOAuth,
		Credentials:           map[string]any{"k": "v"},
		GroupIDs:              []int64{10, 20},
		SkipMixedChannelCheck: true,
	}

	account, err := svc.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, account)
	require.ElementsMatch(t, []int64{10, 20}, invalidator.groupIDs)
}

// TestAdminService_UpdateAccount_InvalidatesUnionOfOldAndNewGroups 验证 Update 时新旧分组并集被失效。
func TestAdminService_UpdateAccount_InvalidatesUnionOfOldAndNewGroups(t *testing.T) {
	existing := &Account{
		ID:       7,
		Name:     "acct-update",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		GroupIDs: []int64{10, 20},
	}
	repo := &accountRepoStubForCreateAndBind{
		accountRepoStubForBulkUpdate: accountRepoStubForBulkUpdate{
			getByIDAccounts: map[int64]*Account{7: existing},
		},
	}
	groupRepo := &groupRepoStubForAdminInvalidate{}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		accountRepo:          repo,
		groupRepo:            groupRepo,
		authCacheInvalidator: invalidator,
	}

	newGroups := []int64{20, 30}
	input := &UpdateAccountInput{
		GroupIDs:              &newGroups,
		SkipMixedChannelCheck: true,
	}

	_, err := svc.UpdateAccount(context.Background(), 7, input)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{10, 20, 30}, invalidator.groupIDs)
}

// TestAdminService_UpdateAccount_NoInvalidationWhenGroupIDsNil 验证未传 GroupIDs 时不触发失效。
func TestAdminService_UpdateAccount_NoInvalidationWhenGroupIDsNil(t *testing.T) {
	existing := &Account{
		ID:       7,
		Name:     "acct-update-noop",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		GroupIDs: []int64{10},
	}
	repo := &accountRepoStubForCreateAndBind{
		accountRepoStubForBulkUpdate: accountRepoStubForBulkUpdate{
			getByIDAccounts: map[int64]*Account{7: existing},
		},
	}
	groupRepo := &groupRepoStubForAdminInvalidate{}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		accountRepo:          repo,
		groupRepo:            groupRepo,
		authCacheInvalidator: invalidator,
	}

	input := &UpdateAccountInput{
		Name: "renamed",
	}

	_, err := svc.UpdateAccount(context.Background(), 7, input)
	require.NoError(t, err)
	require.Empty(t, invalidator.groupIDs)
}

// TestAdminService_BulkUpdateAccounts_InvalidatesUnionOfOldAndNewGroupsPerAccount
// 验证 BulkUpdate 时每个账号的旧分组与新分组并集都被失效。
func TestAdminService_BulkUpdateAccounts_InvalidatesUnionOfOldAndNewGroupsPerAccount(t *testing.T) {
	// 两个账号：account 1 旧绑定 [10]，account 2 旧绑定 [20]。
	// 新绑定 [30] 后，应失效 {10, 20, 30}。
	repo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			1: {ID: 1, Platform: PlatformAnthropic, GroupIDs: []int64{10}},
			2: {ID: 2, Platform: PlatformAnthropic, GroupIDs: []int64{20}},
		},
		getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformAnthropic, GroupIDs: []int64{10}},
			{ID: 2, Platform: PlatformAnthropic, GroupIDs: []int64{20}},
		},
	}
	groupRepo := &groupRepoStubForAdminInvalidate{}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		accountRepo:          repo,
		groupRepo:            groupRepo,
		authCacheInvalidator: invalidator,
	}

	groupIDs := []int64{30}
	input := &BulkUpdateAccountsInput{
		AccountIDs:            []int64{1, 2},
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.ElementsMatch(t, []int64{10, 20, 30}, invalidator.groupIDs)
}

// 静态接口断言以确保 stub 满足 GroupRepository（编译期检查）。
var _ GroupRepository = (*groupRepoStubForAdminInvalidate)(nil)

// 静态接口断言：避免 pagination.PaginationParams 未使用造成 lint 报错。
var _ pagination.PaginationParams = pagination.PaginationParams{}
