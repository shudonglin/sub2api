package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	dbent "github.com/shudonglin/sub2api/ent"
	"github.com/shudonglin/sub2api/ent/enttest"
	"github.com/shudonglin/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newSettingRepoSQLite(t *testing.T) (*settingRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:setting_repo_cache?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &settingRepository{
		client: client,
		cache:  make(map[string]settingCacheEntry, 8),
	}, client
}

// breakClient swaps the repo's ent client for one whose underlying DB has
// been closed. Any read that escapes the cache will immediately fail with a
// driver-level error, which lets each test prove the cache short-circuited
// the DB. We can't DROP the settings table directly because the ent client
// doesn't expose a raw exec helper here, but closing the underlying *sql.DB
// has the same observable effect.
func breakClient(t *testing.T, repo *settingRepository) {
	t.Helper()
	require.NoError(t, repo.client.Close())
}

func TestSettingRepo_GetValue_CachesPositiveResult(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	require.NoError(t, repo.Set(ctx, "k1", "v1"))

	// First read: warms the cache (also written by Set above).
	got, err := repo.GetValue(ctx, "k1")
	require.NoError(t, err)
	require.Equal(t, "v1", got)

	// Close the underlying DB. The cache must still answer.
	breakClient(t, repo)

	got, err = repo.GetValue(ctx, "k1")
	require.NoError(t, err, "second read should hit cache, not DB")
	require.Equal(t, "v1", got)
}

func TestSettingRepo_GetValue_CachesNegativeResult(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	// Missing key — populates the negative cache.
	_, err := repo.GetValue(ctx, "missing-key")
	require.Error(t, err)
	require.True(t, errors.Is(err, service.ErrSettingNotFound))

	// If the negative cache is working, subsequent reads must not touch the DB.
	breakClient(t, repo)

	_, err = repo.GetValue(ctx, "missing-key")
	require.Error(t, err, "still returns not-found from cache")
	require.True(t, errors.Is(err, service.ErrSettingNotFound))
}

func TestSettingRepo_Set_RefreshesCache(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	require.NoError(t, repo.Set(ctx, "k1", "v1"))
	got, err := repo.GetValue(ctx, "k1")
	require.NoError(t, err)
	require.Equal(t, "v1", got)

	// Overwrite — cache must reflect the new value immediately, without waiting
	// for the TTL.
	require.NoError(t, repo.Set(ctx, "k1", "v2"))
	got, err = repo.GetValue(ctx, "k1")
	require.NoError(t, err)
	require.Equal(t, "v2", got, "Set must refresh the cache, not just invalidate")
}

func TestSettingRepo_GetMultiple_PopulatesPerKeyCache(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	require.NoError(t, repo.Set(ctx, "a", "1"))
	require.NoError(t, repo.Set(ctx, "b", "2"))

	res, err := repo.GetMultiple(ctx, []string{"a", "b", "missing"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"a": "1", "b": "2"}, res)

	// All three keys (including the missing one) should now be cached.
	// Prove it by closing the DB and asking again.
	breakClient(t, repo)

	got, err := repo.GetValue(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, "1", got)

	got, err = repo.GetValue(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, "2", got)

	_, err = repo.GetValue(ctx, "missing")
	require.True(t, errors.Is(err, service.ErrSettingNotFound))
}

func TestSettingRepo_Delete_NegativeCachesKey(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	require.NoError(t, repo.Set(ctx, "k1", "v1"))
	require.NoError(t, repo.Delete(ctx, "k1"))

	_, err := repo.GetValue(ctx, "k1")
	require.True(t, errors.Is(err, service.ErrSettingNotFound))
}

func TestSettingRepo_CacheExpires(t *testing.T) {
	repo, _ := newSettingRepoSQLite(t)
	ctx := context.Background()

	// Manually plant a stale entry — far in the past.
	repo.cacheStore("expired-key", settingCacheEntry{
		value:     "stale",
		expiresAt: time.Now().Add(-time.Hour),
	})

	// Stale entries are treated as misses; falling through to the DB yields
	// not-found because no row was written.
	_, err := repo.GetValue(ctx, "expired-key")
	require.True(t, errors.Is(err, service.ErrSettingNotFound))
}
