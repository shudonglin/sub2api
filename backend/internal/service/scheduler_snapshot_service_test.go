//go:build unit

// Phase 6 — defaultBuckets prewarming tests for multi-platform groups.
// Verifies that defaultBuckets seeds buckets per DISTINCT account.platform per group
// (not just per Group.Platform), with byte-level identical seeding for single-platform
// groups and a defensive fallback to Group.Platform when GetAccountPlatforms errors.

package service

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/shudonglin/sub2api/internal/pkg/pagination"
)

// groupRepoStubForDefaultBuckets is a minimal GroupRepository stub used to exercise
// SchedulerSnapshotService.defaultBuckets. ListActive returns the configured groups.
// GetAccountPlatforms returns per-group account platforms (or a forced error).
type groupRepoStubForDefaultBuckets struct {
	groups                  []Group
	accountPlatformsByGroup map[int64][]string
	getAccountPlatformsErr  error
}

func (s *groupRepoStubForDefaultBuckets) Create(_ context.Context, _ *Group) error {
	panic("unexpected Create")
}

func (s *groupRepoStubForDefaultBuckets) GetByID(_ context.Context, _ int64) (*Group, error) {
	panic("unexpected GetByID")
}

func (s *groupRepoStubForDefaultBuckets) GetByIDLite(_ context.Context, _ int64) (*Group, error) {
	panic("unexpected GetByIDLite")
}

func (s *groupRepoStubForDefaultBuckets) Update(_ context.Context, _ *Group) error {
	panic("unexpected Update")
}

func (s *groupRepoStubForDefaultBuckets) Delete(_ context.Context, _ int64) error {
	panic("unexpected Delete")
}

func (s *groupRepoStubForDefaultBuckets) DeleteCascade(_ context.Context, _ int64) ([]int64, error) {
	panic("unexpected DeleteCascade")
}

func (s *groupRepoStubForDefaultBuckets) List(_ context.Context, _ pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List")
}

func (s *groupRepoStubForDefaultBuckets) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _ string, _ *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters")
}

func (s *groupRepoStubForDefaultBuckets) ListActive(_ context.Context) ([]Group, error) {
	out := make([]Group, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func (s *groupRepoStubForDefaultBuckets) ListActiveByPlatform(_ context.Context, _ string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform")
}

func (s *groupRepoStubForDefaultBuckets) ExistsByName(_ context.Context, _ string) (bool, error) {
	panic("unexpected ExistsByName")
}

func (s *groupRepoStubForDefaultBuckets) GetAccountCount(_ context.Context, _ int64) (int64, int64, error) {
	panic("unexpected GetAccountCount")
}

func (s *groupRepoStubForDefaultBuckets) GetAccountPlatforms(_ context.Context, groupID int64) ([]string, error) {
	if s.getAccountPlatformsErr != nil {
		return nil, s.getAccountPlatformsErr
	}
	if platforms, ok := s.accountPlatformsByGroup[groupID]; ok {
		return platforms, nil
	}
	return []string{}, nil
}

func (s *groupRepoStubForDefaultBuckets) DeleteAccountGroupsByGroupID(_ context.Context, _ int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID")
}

func (s *groupRepoStubForDefaultBuckets) GetAccountIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs")
}

func (s *groupRepoStubForDefaultBuckets) BindAccountsToGroup(_ context.Context, _ int64, _ []int64) error {
	panic("unexpected BindAccountsToGroup")
}

func (s *groupRepoStubForDefaultBuckets) UpdateSortOrders(_ context.Context, _ []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders")
}

var _ GroupRepository = (*groupRepoStubForDefaultBuckets)(nil)

// bucketKeysForGroup extracts and sorts bucket String() keys for the given groupID.
func bucketKeysForGroup(buckets []SchedulerBucket, groupID int64) []string {
	keys := make([]string, 0, len(buckets))
	for _, b := range buckets {
		if b.GroupID == groupID {
			keys = append(keys, b.String())
		}
	}
	sort.Strings(keys)
	return keys
}

// TestDefaultBuckets_MultiPlatformGroup_SeedsAllAccountPlatforms verifies that a group
// with Platform=openai but bound accounts of [openai, anthropic] receives buckets for
// BOTH platforms (not just group.Platform).
func TestDefaultBuckets_MultiPlatformGroup_SeedsAllAccountPlatforms(t *testing.T) {
	const groupID int64 = 42
	repo := &groupRepoStubForDefaultBuckets{
		groups: []Group{
			{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountPlatformsByGroup: map[int64][]string{
			groupID: {PlatformOpenAI, PlatformAnthropic},
		},
	}
	svc := &SchedulerSnapshotService{groupRepo: repo}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets returned error: %v", err)
	}

	got := bucketKeysForGroup(buckets, groupID)
	// openai → single + forced (no mixed). anthropic → single + forced + mixed.
	want := []string{
		"42:anthropic:forced",
		"42:anthropic:mixed",
		"42:anthropic:single",
		"42:openai:forced",
		"42:openai:single",
	}
	if !equalStringSlices(got, want) {
		t.Fatalf("multi-platform bucket seed mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestDefaultBuckets_SinglePlatformGroup_IdenticalSeed is the regression guard: a group
// whose only bound account platform matches Group.Platform must produce byte-identical
// buckets to the pre-Phase-6 behaviour.
func TestDefaultBuckets_SinglePlatformGroup_IdenticalSeed(t *testing.T) {
	const groupID int64 = 7
	repo := &groupRepoStubForDefaultBuckets{
		groups: []Group{
			{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive},
		},
		accountPlatformsByGroup: map[int64][]string{
			groupID: {PlatformAnthropic},
		},
	}
	svc := &SchedulerSnapshotService{groupRepo: repo}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets returned error: %v", err)
	}

	got := bucketKeysForGroup(buckets, groupID)
	want := []string{
		"7:anthropic:forced",
		"7:anthropic:mixed",
		"7:anthropic:single",
	}
	if !equalStringSlices(got, want) {
		t.Fatalf("single-platform bucket seed regression:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestDefaultBuckets_GetAccountPlatformsErrors_FallsBackToGroupPlatform verifies that a
// query failure does not break startup — the group should still get buckets seeded for
// its Group.Platform fallback only.
func TestDefaultBuckets_GetAccountPlatformsErrors_FallsBackToGroupPlatform(t *testing.T) {
	const groupID int64 = 11
	repo := &groupRepoStubForDefaultBuckets{
		groups: []Group{
			{ID: groupID, Platform: PlatformGemini, Status: StatusActive},
		},
		getAccountPlatformsErr: errors.New("boom"),
	}
	svc := &SchedulerSnapshotService{groupRepo: repo}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets returned error: %v", err)
	}

	got := bucketKeysForGroup(buckets, groupID)
	want := []string{
		"11:gemini:forced",
		"11:gemini:mixed",
		"11:gemini:single",
	}
	if !equalStringSlices(got, want) {
		t.Fatalf("error-fallback bucket seed mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestDefaultBuckets_EmptyAccountPlatforms_FallsBackToGroupPlatform verifies that when
// a group has zero bound accounts (GetAccountPlatforms returns empty slice), we still
// seed Group.Platform buckets so the group isn't completely cold.
func TestDefaultBuckets_EmptyAccountPlatforms_FallsBackToGroupPlatform(t *testing.T) {
	const groupID int64 = 13
	repo := &groupRepoStubForDefaultBuckets{
		groups: []Group{
			{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountPlatformsByGroup: map[int64][]string{
			groupID: {},
		},
	}
	svc := &SchedulerSnapshotService{groupRepo: repo}

	buckets, err := svc.defaultBuckets(context.Background())
	if err != nil {
		t.Fatalf("defaultBuckets returned error: %v", err)
	}

	got := bucketKeysForGroup(buckets, groupID)
	want := []string{
		"13:openai:forced",
		"13:openai:single",
	}
	if !equalStringSlices(got, want) {
		t.Fatalf("empty-platforms fallback bucket seed mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
