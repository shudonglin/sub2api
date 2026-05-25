package repository

import (
	"context"
	"sync"
	"time"

	"github.com/shudonglin/sub2api/ent"
	"github.com/shudonglin/sub2api/ent/setting"
	"github.com/shudonglin/sub2api/internal/service"
)

// settingCacheTTL is the freshness window for the in-process settings cache.
// Settings change rarely (operator action via admin UI / SetMultiple migration),
// and stale reads for a few seconds are harmless across the codebase — but
// every uncached lookup was crossing the network to the database. On a
// modestly-deployed instance this layer was driving 70+ qps against Supabase
// (≈99.5% of them returning zero rows because the lookup was for a missing
// optional key); the cache, with negative-result caching, collapses that to
// effectively zero steady-state qps.
//
// 30s is a deliberate trade-off: short enough that operator-driven setting
// flips propagate within a normal page reload, long enough to absorb hot
// loops (alert evaluator, gateway hot path, prometheus scrape side-effects)
// without ever re-issuing the query. Write paths (Set / SetMultiple / Delete)
// invalidate immediately so explicit updates are not subject to TTL.
const settingCacheTTL = 30 * time.Second

// settingCacheEntry holds either a hit (`value` populated, `missing=false`)
// or a negative cache marker (`missing=true`). Negative caching is essential
// here: in real deployments most of the hottest keys are *absent*, and a
// non-negative cache would still do a DB round-trip on every call.
type settingCacheEntry struct {
	value     string
	missing   bool
	expiresAt time.Time
}

type settingRepository struct {
	client *ent.Client

	cacheMu sync.RWMutex
	cache   map[string]settingCacheEntry
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{
		client: client,
		cache:  make(map[string]settingCacheEntry, 64),
	}
}

// cacheLoad returns (entry, true) if a fresh entry exists, otherwise zero, false.
// Stale entries are treated as misses; we don't actively prune to keep this
// fast-path branchless beyond the timestamp check.
func (r *settingRepository) cacheLoad(key string) (settingCacheEntry, bool) {
	r.cacheMu.RLock()
	e, ok := r.cache[key]
	r.cacheMu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return settingCacheEntry{}, false
	}
	return e, true
}

func (r *settingRepository) cacheStore(key string, e settingCacheEntry) {
	r.cacheMu.Lock()
	r.cache[key] = e
	r.cacheMu.Unlock()
}

// cacheInvalidate drops a key. Used by all write paths so explicit updates
// don't have to wait for the TTL.
func (r *settingRepository) cacheInvalidate(keys ...string) {
	if len(keys) == 0 {
		return
	}
	r.cacheMu.Lock()
	for _, k := range keys {
		delete(r.cache, k)
	}
	r.cacheMu.Unlock()
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	if e, ok := r.cacheLoad(key); ok {
		if e.missing {
			return nil, service.ErrSettingNotFound
		}
		// We don't cache the row's UpdatedAt because the cache's primary purpose
		// is collapsing the hot read path; the only caller that needs UpdatedAt
		// is admin tooling, which can tolerate a TTL-bounded delay.
		return &service.Setting{Key: key, Value: e.value}, nil
	}

	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			r.cacheStore(key, settingCacheEntry{missing: true, expiresAt: time.Now().Add(settingCacheTTL)})
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	r.cacheStore(key, settingCacheEntry{value: m.Value, expiresAt: time.Now().Add(settingCacheTTL)})
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	err := r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return err
	}
	// Refresh the cache directly rather than just invalidating: the next
	// reader (often the admin handler that just wrote) sees the new value
	// immediately, without a redundant round-trip.
	r.cacheStore(key, settingCacheEntry{value: value, expiresAt: time.Now().Add(settingCacheTTL)})
	return nil
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(settings))
	// Build a hit set so we can negative-cache the missing keys in the same pass.
	found := make(map[string]struct{}, len(settings))
	exp := time.Now().Add(settingCacheTTL)
	for _, s := range settings {
		result[s.Key] = s.Value
		found[s.Key] = struct{}{}
		r.cacheStore(s.Key, settingCacheEntry{value: s.Value, expiresAt: exp})
	}
	for _, k := range keys {
		if _, ok := found[k]; !ok {
			r.cacheStore(k, settingCacheEntry{missing: true, expiresAt: exp})
		}
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	if err := r.client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return err
	}
	exp := time.Now().Add(settingCacheTTL)
	for key, value := range settings {
		r.cacheStore(key, settingCacheEntry{value: value, expiresAt: exp})
	}
	return nil
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(settings))
	exp := time.Now().Add(settingCacheTTL)
	for _, s := range settings {
		result[s.Key] = s.Value
		// Opportunistically populate the per-key cache too; subsequent
		// single-key reads (the hot path) will hit it without another query.
		r.cacheStore(s.Key, settingCacheEntry{value: s.Value, expiresAt: exp})
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	if err == nil {
		// Switch the cache to a negative entry rather than dropping outright,
		// so callers querying the just-deleted key still benefit from the
		// negative cache and don't re-hit the DB.
		r.cacheStore(key, settingCacheEntry{missing: true, expiresAt: time.Now().Add(settingCacheTTL)})
	}
	return err
}
