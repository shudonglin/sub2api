# Anti-Detection Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Comprehensive mitigation of all Anthropic upstream detection signals that can identify sub2api as a shared subscription gateway, covering PR #1447 scope plus deeper hardening not addressed by that PR.

**Architecture:** Extends the existing `GetGatewayForwardingSettings()` cache + singleflight pattern with three new setting keys. Adds `builtinBlocked` response header set, metadata user_id anonymization, privacy mode master switch, attribution header computation, and fingerprint diversity. All changes are backwards-compatible with existing deployments (privacy mode defaults to enabled).

**Tech Stack:** Go 1.26, Ent ORM, Gin, Redis (identity cache), gjson/sjson (JSON manipulation)

---

## Threat Model Summary

Based on Claude Code source analysis (`src/services/api/claude.ts`, `src/constants/system.ts`, `src/services/api/logging.ts`, `src/services/api/client.ts`):

| # | Detection Signal | Severity | Current Status |
|---|---|---|---|
| 1 | `cch=` Native Client Attestation (Zig binary hash) | 🔴 Fatal | **Cannot mitigate** — requires genuine Claude Code binary. Currently behind feature flag, not enforced. |
| 2 | `metadata.user_id` cross-correlation (device_id/account_uuid cardinality) | 🔴 High | Partially mitigated (RewriteUserID exists, but no anonymization) |
| 3 | Attribution header fingerprint (`cc_version.{hash}; cc_entrypoint=`) | 🟠 High | **Not handled** — no computation or stripping |
| 4 | `X-Stainless-*` fingerprint uniformity (all accounts identical) | 🟠 High | Static `DefaultHeaders` — all mimic clients identical |
| 5 | Upstream tracking response headers (Set-Cookie, Report-To, NEL) | 🟡 Medium | Whitelist-only, but no explicit blocking of tracking headers |
| 6 | Session topology (high-frequency session creation per account) | 🟡 Medium | Per-account session masking exists but opt-in only |
| 7 | Gateway header leakage (sub2api adding identifiable headers) | 🟢 Low | Mostly clean, but no audit |

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `backend/internal/service/domain_constants.go` | Modify | Add 3 new setting keys |
| `backend/internal/service/settings_view.go` | Modify | Add 3 new fields to `SystemSettings` |
| `backend/internal/service/setting_service.go` | Modify | Extend cache struct, `GetGatewayForwardingSettings`, `parseSettings`, `UpdateSettings` |
| `backend/internal/service/identity_service.go` | Modify | Add `anonymize` param to `RewriteUserID`, `maskingOverride` to `RewriteUserIDWithMasking`, add `generateClientIDFromSeed` |
| `backend/internal/service/gateway_service.go` | Modify | Thread new settings through `Forward`, `buildUpstreamRequest`, `buildOAuthMetadataUserID`, `buildCountTokensRequest` |
| `backend/internal/handler/admin/setting_handler.go` | Modify | Add new fields to `GetSettings`, `UpdateSettings`, `diffSettings` |
| `backend/internal/handler/dto/settings.go` | Modify | Add new fields to DTO `SystemSettings` |
| `backend/internal/util/responseheaders/responseheaders.go` | Modify | Add `builtinBlocked` set |
| `backend/internal/util/responseheaders/responseheaders_test.go` | Modify | Add tests for builtin blocking |
| `backend/internal/service/gateway_oauth_metadata_test.go` | Modify | Update test signatures |
| `backend/internal/service/identity_service_order_test.go` | Modify | Update test signatures |

---

## Task 1: Add `builtinBlocked` response headers (standalone, no dependencies)

**Files:**
- Modify: `backend/internal/util/responseheaders/responseheaders.go`
- Modify: `backend/internal/util/responseheaders/responseheaders_test.go`

**Rationale:** Anthropic can inject `Set-Cookie`, `Report-To`, `NEL`, `Alt-Svc`, `Server-Timing`, `Origin-Agent-Cluster` response headers to track clients. These must be unconditionally stripped, even if `additional_allowed` tries to re-enable them.

- [ ] **Step 1: Add `builtinBlocked` set in responseheaders.go**

After the `defaultAllowed` map, add:

```go
// builtinBlocked is a hardcoded set of response headers that are always stripped,
// regardless of additional_allowed configuration. These headers can be used by
// upstream providers to track client identity or inject cookies.
var builtinBlocked = map[string]struct{}{
    "set-cookie":           {},
    "alt-svc":              {},
    "server-timing":        {},
    "report-to":            {},
    "nel":                  {},
    "origin-agent-cluster": {},
}
```

- [ ] **Step 2: Integrate `builtinBlocked` into `CompileHeaderFilter`**

Change `forceRemove` initialization to seed from `builtinBlocked`:

```go
forceRemove := make(map[string]struct{}, len(builtinBlocked)+len(cfg.ForceRemove))
for key := range builtinBlocked {
    forceRemove[key] = struct{}{}
}
if cfg.Enabled {
    for _, key := range cfg.ForceRemove { ... }
}
```

- [ ] **Step 3: Write tests**

Add two tests:
1. `TestFilterHeadersBuiltinBlockedAlwaysRemoved` — Set-Cookie/Alt-Svc/etc removed even with default config
2. `TestFilterHeadersAdditionalAllowedCannotReenableBuiltinBlocked` — explicit `additional_allowed: ["set-cookie"]` still blocked

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/util/responseheaders/... -v
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(privacy): add builtinBlocked response headers — unconditionally strip upstream tracking headers"
```

---

## Task 2: Add new setting keys and extend structs

**Files:**
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/handler/dto/settings.go`

- [ ] **Step 1: Add setting keys to domain_constants.go**

After `SettingKeyEnableMetadataPassthrough`, add:

```go
// SettingKeyEnableMetadataUserIDAnonymization controls whether device_id and account_uuid
// in metadata.user_id are replaced with stable pseudonyms (SHA-256 derived from account ID).
// Default: false. When enabled, upstream cannot correlate requests to real devices/accounts.
SettingKeyEnableMetadataUserIDAnonymization = "enable_metadata_userid_anonymization"

// SettingKeyEnablePrivacyMode is the master privacy switch (default: true).
// When enabled, forces: fingerprint unification + metadata anonymization + session masking.
SettingKeyEnablePrivacyMode = "enable_privacy_mode"
```

- [ ] **Step 2: Add fields to `SystemSettings` in settings_view.go**

```go
EnableMetadataUserIDAnonymization bool // anonymize device_id/account_uuid (default false)
EnablePrivacyMode                 bool // master switch: forces FP unification + anonymization + session masking (default true)
```

- [ ] **Step 3: Add fields to DTO in dto/settings.go**

```go
EnableMetadataUserIDAnonymization bool `json:"enable_metadata_userid_anonymization"`
EnablePrivacyMode                 bool `json:"enable_privacy_mode"`
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat(privacy): add EnableMetadataUserIDAnonymization and EnablePrivacyMode setting keys"
```

---

## Task 3: Extend setting_service.go — cache, read, write, parse

**Files:**
- Modify: `backend/internal/service/setting_service.go`

- [ ] **Step 1: Extend `cachedGatewayForwardingSettings` struct**

```go
type cachedGatewayForwardingSettings struct {
    fingerprintUnification      bool
    metadataPassthrough         bool
    metadataUserIDAnonymization bool
    privacyMode                 bool
    expiresAt                   int64
}
```

- [ ] **Step 2: Update `GetGatewayForwardingSettings` signature and logic**

New signature:
```go
func (s *SettingService) GetGatewayForwardingSettings(ctx context.Context) (fingerprintUnification, metadataPassthrough, metadataUserIDAnonymization, privacyMode bool)
```

Key logic: when `privacyMode` is true, force `fingerprintUnification = true` and `metadataUserIDAnonymization = true`.

Read 4 keys from DB. Privacy mode defaults to `true` when DB has no value.

- [ ] **Step 3: Update `UpdateSettings` to save new keys and refresh cache**

- [ ] **Step 4: Update `parseSettings` to read new keys**

Privacy mode default: `true` when DB has no value.

- [ ] **Step 5: Compile check**

```bash
cd backend && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(privacy): extend GetGatewayForwardingSettings with anonymization + privacy mode"
```

---

## Task 4: Extend identity_service.go — anonymization support

**Files:**
- Modify: `backend/internal/service/identity_service.go`

- [ ] **Step 1: Add `generateClientIDFromSeed` helper**

```go
func generateClientIDFromSeed(seed string) string {
    hash := sha256.Sum256([]byte(seed))
    return hex.EncodeToString(hash[:])
}
```

- [ ] **Step 2: Add `anonymize` parameter to `RewriteUserID`**

New signature: `RewriteUserID(body []byte, accountID int64, accountUUID, cachedClientID, fingerprintUA string, anonymize bool) ([]byte, error)`

When `anonymize` is true:
- `cachedClientID` → `generateClientIDFromSeed("metadata-device:{accountID}")`
- `accountUUID` → `generateUUIDFromSeed("metadata-account:{accountID}")`

- [ ] **Step 3: Add `maskingOverride` parameter to `RewriteUserIDWithMasking`**

New signature: `RewriteUserIDWithMasking(ctx context.Context, body []byte, account *Account, accountUUID, cachedClientID, fingerprintUA string, anonymize bool, maskingOverride bool) ([]byte, error)`

When `maskingOverride` is true, skip the `account.IsSessionIDMaskingEnabled()` check and always apply masking.

- [ ] **Step 4: Compile check**

```bash
cd backend && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(privacy): add anonymize and maskingOverride params to identity service"
```

---

## Task 5: Thread new settings through gateway_service.go

**Files:**
- Modify: `backend/internal/service/gateway_service.go`

This task updates all call sites of `GetGatewayForwardingSettings`, `RewriteUserID`, `RewriteUserIDWithMasking`, and `buildOAuthMetadataUserID` to use the new 4-return-value and additional parameters.

- [ ] **Step 1: Update `buildOAuthMetadataUserID` to accept `anonymize bool`**

When anonymize=true, replace `userID` with `generateClientIDFromSeed(...)` and `accountUUID` with `generateUUIDFromSeed(...)`.

- [ ] **Step 2: Update `Forward` method**

Change `_, mimicMPT := s.settingService.GetGatewayForwardingSettings(ctx)` to `_, mimicMPT, mimicMUA, _ := ...` and pass `mimicMUA` to `buildOAuthMetadataUserID`.

- [ ] **Step 3: Update `buildUpstreamRequest`**

Change `enableFP, enableMPT := ...` to `enableFP, enableMPT, enableMUA, enablePM := ...` and pass `enableMUA` + `enablePM` to `RewriteUserIDWithMasking`.

- [ ] **Step 4: Update `buildCountTokensRequest`**

Same pattern as step 3.

- [ ] **Step 5: Compile and test**

```bash
cd backend && go build ./... && go test ./internal/service/... -v -count=1
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(privacy): thread privacy mode settings through gateway service"
```

---

## Task 6: Update admin handler and DTO

**Files:**
- Modify: `backend/internal/handler/admin/setting_handler.go`

- [ ] **Step 1: Add new fields to `GetSettings` response**

- [ ] **Step 2: Add new fields to `UpdateSettingsRequest`**

- [ ] **Step 3: Add new fields to `UpdateSettings` merge logic**

- [ ] **Step 4: Add new fields to response in `UpdateSettings`**

- [ ] **Step 5: Add `diffSettings` entries for new keys**

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(privacy): expose privacy mode and anonymization in admin API"
```

---

## Task 7: Fix existing tests for new signatures

**Files:**
- Modify: `backend/internal/service/gateway_oauth_metadata_test.go`
- Modify: `backend/internal/service/identity_service_order_test.go`

- [ ] **Step 1: Update `buildOAuthMetadataUserID` test calls**

Add `false` (anonymize) parameter to existing test calls.

- [ ] **Step 2: Update `RewriteUserID` test calls**

Add `false` (anonymize) parameter.

- [ ] **Step 3: Update `RewriteUserIDWithMasking` test calls**

Add `false, false` (anonymize, maskingOverride) parameters.

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/service/... ./internal/util/responseheaders/... -v -count=1
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "test: update existing tests for new privacy mode signatures"
```

---

## Execution Priority

Execute tasks in this order (dependency-aware):

1. **Task 1** — Response header blocking (standalone, no deps)
2. **Task 2** — Setting keys + struct fields (foundation)
3. **Task 3** — Setting service cache/read/write (depends on Task 2)
4. **Task 4** — Identity service anonymization (standalone logic)
5. **Task 5** — Gateway service threading (depends on Tasks 3 + 4)
6. **Task 6** — Admin handler (depends on Tasks 2 + 3)
7. **Task 7** — Test fixes (depends on Tasks 4 + 5)

---

## Out of Scope (Future Work)

| Item | Status |
|---|---|
| `cch=` attestation bypass | **Infeasible** — requires Bun/Zig native binary; feature flag not yet enforced |
| Attribution header fingerprint computation | **✅ DONE** — `attribution.go` computes `SHA256(SALT + msg[4,7,20] + VERSION)[:3]` and injects as first system block |
| Fingerprint diversity pool (per-account X-Stainless-*) | **✅ DONE** — `generateDiverseFingerprint()` in `identity_service.go` — deterministic per-account OS/Arch/Runtime/CLI version selection |
| IP diversity enforcement | Operational concern, not a code change |
| Trusted device token handling during OAuth login | Deferred — requires changes to OAuth flow, high complexity |
