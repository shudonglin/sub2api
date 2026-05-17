//go:build unit

// 账号服务 auth cache 失效测试
// 验证 AccountService.Create / Update 在分组变更时会失效 API Key 认证缓存。

package service

import (
	"context"
	"testing"
	"time"

	"github.com/shudonglin/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// authCacheInvalidatorRecorder 记录所有失效调用，用于断言 group ID 集合。
type authCacheInvalidatorRecorder struct {
	keys                []string
	userIDs             []int64
	invalidatedGroupIDs []int64
}

func (r *authCacheInvalidatorRecorder) InvalidateAuthCacheByKey(_ context.Context, key string) {
	r.keys = append(r.keys, key)
}

func (r *authCacheInvalidatorRecorder) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	r.userIDs = append(r.userIDs, userID)
}

func (r *authCacheInvalidatorRecorder) InvalidateAuthCacheByGroupID(_ context.Context, groupID int64) {
	r.invalidatedGroupIDs = append(r.invalidatedGroupIDs, groupID)
}

// accountRepoStubForInvalidate 是支持 Create/Update/BindGroups/GetByID 的轻量 stub。
// 在 accountRepoStub 上叠加最小行为，避免触发 panic。
type accountRepoStubForInvalidate struct {
	accountRepoStub

	// Create 行为
	createErr error
	created   *Account

	// GetByID 行为
	getByIDAccount *Account
	getByIDErr     error

	// Update 行为
	updateErr error
	updated   *Account

	// BindGroups 行为
	bindGroupsErr   error
	bindGroupsCalls []struct {
		AccountID int64
		GroupIDs  []int64
	}
}

func (s *accountRepoStubForInvalidate) Create(_ context.Context, account *Account) error {
	if s.createErr != nil {
		return s.createErr
	}
	if account.ID == 0 {
		account.ID = 999
	}
	s.created = account
	return nil
}

func (s *accountRepoStubForInvalidate) GetByID(_ context.Context, _ int64) (*Account, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	return s.getByIDAccount, nil
}

func (s *accountRepoStubForInvalidate) Update(_ context.Context, account *Account) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = account
	return nil
}

func (s *accountRepoStubForInvalidate) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	if s.bindGroupsErr != nil {
		return s.bindGroupsErr
	}
	s.bindGroupsCalls = append(s.bindGroupsCalls, struct {
		AccountID int64
		GroupIDs  []int64
	}{accountID, append([]int64{}, groupIDs...)})
	return nil
}

// groupRepoStubForInvalidate 满足 GroupRepository 与 groupExistenceBatchChecker。
type groupRepoStubForInvalidate struct {
	existsByID map[int64]bool
}

func (s *groupRepoStubForInvalidate) Create(_ context.Context, _ *Group) error {
	panic("unexpected Create")
}

func (s *groupRepoStubForInvalidate) GetByID(_ context.Context, id int64) (*Group, error) {
	return &Group{ID: id, Platform: PlatformAnthropic}, nil
}

func (s *groupRepoStubForInvalidate) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	return s.GetByID(context.Background(), id)
}

func (s *groupRepoStubForInvalidate) Update(_ context.Context, _ *Group) error {
	panic("unexpected Update")
}

func (s *groupRepoStubForInvalidate) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete")
}

func (s *groupRepoStubForInvalidate) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade")
}

func (s *groupRepoStubForInvalidate) List(_ context.Context, _ pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List")
}

func (s *groupRepoStubForInvalidate) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _ string, _ *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters")
}

func (s *groupRepoStubForInvalidate) ListActive(_ context.Context) ([]Group, error) {
	panic("unexpected ListActive")
}

func (s *groupRepoStubForInvalidate) ListActiveByPlatform(_ context.Context, _ string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform")
}

func (s *groupRepoStubForInvalidate) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName")
}

func (s *groupRepoStubForInvalidate) GetAccountCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetAccountCount")
}

func (s *groupRepoStubForInvalidate) GetAccountPlatforms(_ context.Context, _ int64) ([]string, error) {
	panic("unexpected GetAccountPlatforms")
}

func (s *groupRepoStubForInvalidate) DeleteAccountGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID")
}

func (s *groupRepoStubForInvalidate) GetAccountIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs")
}

func (s *groupRepoStubForInvalidate) BindAccountsToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindAccountsToGroup")
}

func (s *groupRepoStubForInvalidate) UpdateSortOrders(_ context.Context, _ []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders")
}

// ExistsByIDs implements groupExistenceBatchChecker — 用于 validateGroupIDsExist 的快路径。
func (s *groupRepoStubForInvalidate) ExistsByIDs(_ context.Context, ids []int64) (map[int64]bool, error) {
	if s.existsByID != nil {
		out := make(map[int64]bool, len(ids))
		for _, id := range ids {
			out[id] = s.existsByID[id]
		}
		return out, nil
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// 用于 Create 中 require_oauth_only 检查时，避免对 PlatformAnthropic 报错（默认 RequireOAuthOnly=false）

// TestAccountService_Create_InvalidatesAuthCacheForBoundGroups 验证 Create 后所有新分组都被失效。
func TestAccountService_Create_InvalidatesAuthCacheForBoundGroups(t *testing.T) {
	accountRepo := &accountRepoStubForInvalidate{}
	groupRepo := &groupRepoStubForInvalidate{}
	invalidator := &authCacheInvalidatorRecorder{}
	svc := &AccountService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		authCache:   invalidator,
	}

	_, err := svc.Create(context.Background(), CreateAccountRequest{
		Name:        "acct-1",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"foo": "bar"},
		GroupIDs:    []int64{10, 20},
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{10, 20}, invalidator.invalidatedGroupIDs)
}

// TestAccountService_Update_InvalidatesUnionOfOldAndNewGroups 验证 Update 时新旧分组并集被失效。
func TestAccountService_Update_InvalidatesUnionOfOldAndNewGroups(t *testing.T) {
	now := time.Now()
	existing := &Account{
		ID:        7,
		Name:      "acct-7",
		Platform:  PlatformAnthropic,
		Type:      AccountTypeOAuth,
		Status:    StatusActive,
		GroupIDs:  []int64{10, 20},
		CreatedAt: now,
	}
	accountRepo := &accountRepoStubForInvalidate{getByIDAccount: existing}
	groupRepo := &groupRepoStubForInvalidate{}
	invalidator := &authCacheInvalidatorRecorder{}
	svc := &AccountService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		authCache:   invalidator,
	}

	newGroups := []int64{20, 30}
	_, err := svc.Update(context.Background(), 7, UpdateAccountRequest{
		GroupIDs: &newGroups,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{10, 20, 30}, invalidator.invalidatedGroupIDs)
}

// TestAccountService_Update_NoInvalidationWhenGroupIDsNil 验证未传 GroupIDs 时不触发失效。
func TestAccountService_Update_NoInvalidationWhenGroupIDsNil(t *testing.T) {
	now := time.Now()
	existing := &Account{
		ID:        7,
		Name:      "acct-7",
		Platform:  PlatformAnthropic,
		Type:      AccountTypeOAuth,
		Status:    StatusActive,
		GroupIDs:  []int64{10, 20},
		CreatedAt: now,
	}
	accountRepo := &accountRepoStubForInvalidate{getByIDAccount: existing}
	groupRepo := &groupRepoStubForInvalidate{}
	invalidator := &authCacheInvalidatorRecorder{}
	svc := &AccountService{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		authCache:   invalidator,
	}

	newName := "acct-7-renamed"
	_, err := svc.Update(context.Background(), 7, UpdateAccountRequest{
		Name: &newName,
	})
	require.NoError(t, err)
	require.Empty(t, invalidator.invalidatedGroupIDs)
}
