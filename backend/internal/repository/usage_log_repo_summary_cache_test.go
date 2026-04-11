package repository

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSQLExecutor is a minimal sqlExecutor used by the cache-miss tests. We
// don't actually decode rows — the stub just records that QueryContext was
// called and returns a pre-configured error so the slow-path short-circuits.
type stubSQLExecutor struct {
	queryCalls atomic.Int64
	err        error
}

func (s *stubSQLExecutor) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, nil
}

func (s *stubSQLExecutor) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	s.queryCalls.Add(1)
	return nil, s.err
}

// newTestUsageLogRepo creates a minimal usageLogRepository backed by the stub.
// We skip fields like bestEffortRecent that are initialised by the constructor
// because the tested code path does not touch them.
func newTestUsageLogRepo() *usageLogRepository {
	return &usageLogRepository{}
}

// injectCacheEntry writes a summaryCacheEntry into the repo's atomic pointer
// so we can test the fast-path without touching the SQL layer.
func injectCacheEntry(r *usageLogRepository, entry *summaryCacheEntry) {
	r.cachedSummary.Store(entry)
}

// TestGetAllGroupUsageSummary_CacheHitOnSecondCall verifies that a primed cache
// entry is returned directly without calling the singleflight / SQL path.
func TestGetAllGroupUsageSummary_CacheHitOnSecondCall(t *testing.T) {
	r := newTestUsageLogRepo()
	todayStart := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	want := []usagestats.GroupUsageSummary{
		{GroupID: 1, TotalCost: 10.5, TodayCost: 2.3},
		{GroupID: 2, TotalCost: 5.0, TodayCost: 1.1},
	}

	// Prime the cache manually — simulates what the slow-path does.
	injectCacheEntry(r, &summaryCacheEntry{
		todayStart: todayStart,
		results:    want,
		expiresAt:  time.Now().Add(30 * time.Second),
	})

	ctx := context.Background()

	// First call: should be served from cache.
	got1, err := r.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	assert.Equal(t, want, got1)

	// Second call: still within TTL, should also be served from cache.
	got2, err := r.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	assert.Equal(t, want, got2)
}

// TestGetAllGroupUsageSummary_CacheExpiry verifies that an expired entry is
// not served. We don't exercise the SQL path here; we just confirm that once
// the cache expires the fast-path is skipped (a nil-result is returned from the
// singleflight because the stub SQL path returns nil rows — the important thing
// is that the fast-path guard did not return the stale data).
func TestGetAllGroupUsageSummary_CacheExpiry(t *testing.T) {
	r := newTestUsageLogRepo()
	todayStart := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	stale := []usagestats.GroupUsageSummary{
		{GroupID: 99, TotalCost: 999, TodayCost: 999},
	}

	// Inject an already-expired entry.
	injectCacheEntry(r, &summaryCacheEntry{
		todayStart: todayStart,
		results:    stale,
		expiresAt:  time.Now().Add(-1 * time.Second), // expired
	})

	// Wire a stub SQL executor so the uncached path doesn't panic.
	// The stub returns an error ("no rows") — we just care that we did NOT get
	// the stale cached value back.
	stub := &stubSQLExecutor{err: sql.ErrNoRows}
	r.sql = stub

	ctx := context.Background()
	got, err := r.GetAllGroupUsageSummary(ctx, todayStart)

	// The call hit the DB (stub returned ErrNoRows), confirming the cache miss.
	assert.Error(t, err, "expired cache should cause a DB call, which returns an error from the stub")
	assert.Nil(t, got, "stale cached value must not be returned")
	assert.Equal(t, int64(1), stub.queryCalls.Load(), "SQL must have been called exactly once")
}

// TestGetAllGroupUsageSummary_ReturnsCopy verifies that mutating the returned
// slice does not affect subsequent cache reads.
func TestGetAllGroupUsageSummary_ReturnsCopy(t *testing.T) {
	r := newTestUsageLogRepo()
	todayStart := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	original := []usagestats.GroupUsageSummary{
		{GroupID: 1, TotalCost: 10.0, TodayCost: 1.0},
	}

	injectCacheEntry(r, &summaryCacheEntry{
		todayStart: todayStart,
		results:    original,
		expiresAt:  time.Now().Add(30 * time.Second),
	})

	ctx := context.Background()
	got, err := r.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)

	// Mutate the returned slice.
	got[0].TotalCost = 9999

	// A second read from the cache must still return the original value.
	got2, err := r.GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	assert.Equal(t, 10.0, got2[0].TotalCost, "cached value must not be affected by caller mutation")
}
