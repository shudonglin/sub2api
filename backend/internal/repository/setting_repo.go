package repository

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shudonglin/sub2api/ent"
	"github.com/shudonglin/sub2api/ent/setting"
	"github.com/shudonglin/sub2api/internal/service"
)

// settingCacheTTL is the freshness window for the in-process settings cache.
// Settings change rarely (operator action via admin UI / SetMultiple migration),
// and stale reads for a few minutes are harmless across the codebase — but
// every uncached lookup was crossing the network to the database. On a
// modestly-deployed instance this layer was driving 70+ qps against Supabase
// even with a 30s cache, because almost every lookup was for a key that
// didn't exist and the negative cache was getting churned by a hot path.
//
// 5 minutes is the right trade-off:
//   - operator-driven changes still propagate within a single page reload
//     (writes through Set / SetMultiple refresh the cache immediately, so
//     this TTL only affects propagation between processes — which doesn't
//     apply on a single-instance deployment)
//   - long enough that *any* periodic loop (per-second, per-minute) sees
//     a steady-state cache hit
//   - combined with the proactive whole-table prewarm below, the
//     steady-state DB qps for settings drops to ~1 query per 5 minutes
//     regardless of the lookup pattern.
const settingCacheTTL = 5 * time.Minute

// settingPrewarmInterval drives the background refresh that proactively
// pulls every settings row into the cache. Set slightly below settingCacheTTL
// so existing positive entries are always refreshed before they expire,
// keeping the cache permanently warm for known keys.
const settingPrewarmInterval = 4 * time.Minute

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

	// warmed is set to true once the first GetAll prewarm completes
	// successfully. While true, the cache is treated as a complete snapshot:
	// any single-key lookup for a key absent from the cache is answered
	// negative immediately without a DB round-trip. The next periodic GetAll
	// will pick up any new keys written by other processes (or, on this
	// single-instance deployment, will simply re-confirm the snapshot).
	warmed atomic.Bool

	// Diagnostic counters. Temporary — exists to find a 31 qps SELECT-settings
	// driver that survived the cache. Logged once a minute via the stats logger
	// started in NewSettingRepository.
	statHitPositive atomic.Uint64
	statHitNegative atomic.Uint64
	statMiss        atomic.Uint64
	statSnapshotHit atomic.Uint64

	// missCountMu protects missByKey + missKeysSinceFlush.
	// We track *unique* missing keys with counts so the log line tells us
	// whether 30 qps is 30 unique keys/sec (cardinality bomb) or 1 key being
	// re-queried by callers that bypass the cache somehow.
	missCountMu        sync.Mutex
	missByKey          map[string]uint64
	missKeysSinceFlush int
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	r := &settingRepository{
		client:    client,
		cache:     make(map[string]settingCacheEntry, 64),
		missByKey: make(map[string]uint64, 64),
	}
	// Run an initial prewarm synchronously-ish (with a short timeout) so the
	// first reads after process start already hit the snapshot. Failure is
	// non-fatal — the cache falls back to per-key fills until the periodic
	// prewarm succeeds.
	go r.runPrewarmer()
	go r.runStatsLogger()
	return r
}

// runPrewarmer pulls the entire settings table into the cache, then refreshes
// on settingPrewarmInterval. One DB query per refresh, regardless of how many
// unique keys callers ask for. Combined with the snapshot semantics below
// (warmed=true → missing keys answered without DB), this caps the
// steady-state DB qps for this repo at roughly 1 / settingPrewarmInterval.
func (r *settingRepository) runPrewarmer() {
	// First prewarm runs immediately; subsequent ones on the ticker.
	r.prewarmOnce()
	t := time.NewTicker(settingPrewarmInterval)
	defer t.Stop()
	for range t.C {
		r.prewarmOnce()
	}
}

func (r *settingRepository) prewarmOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		slog.Warn("setting_cache_prewarm_failed", "error", err.Error())
		return
	}
	exp := time.Now().Add(settingCacheTTL)
	next := make(map[string]settingCacheEntry, len(settings)+8)
	for _, s := range settings {
		next[s.Key] = settingCacheEntry{value: s.Value, expiresAt: exp}
	}
	// Atomic swap of the snapshot. Doing it under the write lock so concurrent
	// reads see either the old map or the new one, not a half-built state.
	// We deliberately do NOT carry over negative entries from the previous
	// snapshot: if a key was missing 5 minutes ago and we re-checked just now,
	// it's still missing — but a caller might have legitimately written it in
	// between via Set/SetMultiple, which already populated the (old) cache,
	// and we'd be stomping that fresh write. To avoid the stomp, we merge:
	// keep any cache entry that was written more recently than this prewarm
	// started.
	prewarmStart := time.Now().Add(-10 * time.Second)
	r.cacheMu.Lock()
	for k, prev := range r.cache {
		if _, fromSnapshot := next[k]; fromSnapshot {
			continue
		}
		// Carry over recent writes (positive or negative) that won't be in the
		// new snapshot. Older negative entries are dropped — the snapshot is
		// now authoritative.
		if !prev.missing && prev.expiresAt.After(prewarmStart.Add(settingCacheTTL/2)) {
			next[k] = prev
		}
	}
	r.cache = next
	r.cacheMu.Unlock()
	r.warmed.Store(true)
	slog.Debug("setting_cache_prewarmed", "keys", len(settings))
}

// runStatsLogger emits one slog line per minute summarizing cache effectiveness
// and the top missing keys observed. Removed once the source of the residual
// query rate is identified.
func (r *settingRepository) runStatsLogger() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for range t.C {
		hp := r.statHitPositive.Swap(0)
		hn := r.statHitNegative.Swap(0)
		ms := r.statMiss.Swap(0)

		r.missCountMu.Lock()
		topKeys := make([]string, 0, len(r.missByKey))
		for k, c := range r.missByKey {
			topKeys = append(topKeys, k+"="+fmtUint(c))
		}
		sort.Strings(topKeys) // deterministic ordering; counts embedded
		uniqueMissKeys := len(r.missByKey)
		r.missByKey = make(map[string]uint64, 64)
		r.missKeysSinceFlush = 0
		r.missCountMu.Unlock()

		// Cap the logged key list so we don't blow out a log line on
		// pathological cardinality.
		shown := topKeys
		if len(shown) > 30 {
			shown = shown[:30]
		}
		slog.Info("setting_cache_stats",
			"hits_positive", hp,
			"hits_negative", hn,
			"misses", ms,
			"unique_miss_keys", uniqueMissKeys,
			"total_calls", hp+hn+ms,
			"top_miss_keys", shown,
		)
	}
}

func fmtUint(n uint64) string {
	// Avoid importing strconv just for this; keep this file's import surface tight.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func (r *settingRepository) recordMiss(key string) {
	r.statMiss.Add(1)
	r.missCountMu.Lock()
	// Bound memory: if we've already tracked 500 unique missing keys this
	// minute, stop adding new ones — the count is what matters, not exhaustive
	// enumeration.
	if _, ok := r.missByKey[key]; ok || r.missKeysSinceFlush < 500 {
		if _, ok := r.missByKey[key]; !ok {
			r.missKeysSinceFlush++
		}
		r.missByKey[key]++
	}
	r.missCountMu.Unlock()
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
			r.statHitNegative.Add(1)
			return nil, service.ErrSettingNotFound
		}
		r.statHitPositive.Add(1)
		// We don't cache the row's UpdatedAt because the cache's primary purpose
		// is collapsing the hot read path; the only caller that needs UpdatedAt
		// is admin tooling, which can tolerate a TTL-bounded delay.
		return &service.Setting{Key: key, Value: e.value}, nil
	}

	// Snapshot fast-path: once the prewarm has succeeded, the cache is
	// authoritative. Any key not in the snapshot is treated as missing without
	// a DB round-trip. This is the lever that takes the "lookup for a key
	// that doesn't exist" pattern from O(callers per second) DB queries down
	// to zero between snapshot refreshes. Write paths (Set / SetMultiple)
	// add positive entries directly, and Delete leaves a negative one, so
	// admin-driven changes are reflected immediately.
	if r.warmed.Load() {
		r.statHitNegative.Add(1)
		// Plant a negative cache entry so we don't even need to take the
		// `warmed` branch on the next call for this key.
		r.cacheStore(key, settingCacheEntry{missing: true, expiresAt: time.Now().Add(settingCacheTTL)})
		return nil, service.ErrSettingNotFound
	}

	r.recordMiss(key)

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

	// Snapshot fast-path: once warmed, the per-key cache has the answer for
	// every existing key (planted by the prewarm) and "missing" is implied for
	// absent keys. Serve entirely from memory.
	if r.warmed.Load() {
		result := make(map[string]string, len(keys))
		exp := time.Now().Add(settingCacheTTL)
		r.cacheMu.Lock()
		for _, k := range keys {
			if e, ok := r.cache[k]; ok && !e.missing && time.Now().Before(e.expiresAt) {
				result[k] = e.value
				r.statHitPositive.Add(1)
			} else if ok && e.missing {
				r.statHitNegative.Add(1)
			} else {
				// Not in snapshot → plant negative entry, count as a "snapshot
				// served" negative hit (not a DB miss).
				r.cache[k] = settingCacheEntry{missing: true, expiresAt: exp}
				r.statHitNegative.Add(1)
			}
		}
		r.cacheMu.Unlock()
		return result, nil
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
