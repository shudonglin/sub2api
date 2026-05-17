/**
 * Group <-> Account platform compatibility helpers.
 *
 * Spec: docs/specs/2026-05-04-multi-platform-groups-design.md (Change 8e)
 *
 * Same-platform attaches are always allowed. Cross-platform attaches are
 * permitted only between the OpenAI family (`openai`, `azure`) and
 * `anthropic`. Gemini and antigravity remain isolated to their own
 * platform per scope.
 */

export interface PlatformLike {
  platform?: string | null
}

const isOpenAIish = (platform?: string | null): boolean =>
  platform === 'openai' || platform === 'azure'

const isAnthropic = (platform?: string | null): boolean =>
  platform === 'anthropic'

/**
 * Returns true when an account may be attached to a group with the given
 * platform.
 */
export function isAccountEligibleForGroup(
  account: PlatformLike | null | undefined,
  group: PlatformLike | null | undefined
): boolean {
  if (!account || !group) return false
  const accountPlatform = account.platform ?? ''
  const groupPlatform = group.platform ?? ''
  if (!accountPlatform || !groupPlatform) return false
  if (accountPlatform === groupPlatform) return true
  if (isOpenAIish(groupPlatform) && isAnthropic(accountPlatform)) return true
  if (isAnthropic(groupPlatform) && isOpenAIish(accountPlatform)) return true
  return false
}

/**
 * Returns the list of account platforms eligible for a group with the
 * given platform. Order is stable: the group's own platform first, then
 * any cross-platform allies.
 */
export function getEligibleAccountPlatforms(groupPlatform?: string | null): string[] {
  if (!groupPlatform) return []
  if (isOpenAIish(groupPlatform)) {
    // openai-ish groups accept their own platform plus anthropic
    return [groupPlatform, 'anthropic']
  }
  if (isAnthropic(groupPlatform)) {
    // anthropic groups accept anthropic + openai-ish (azure is reserved
    // for forward-compat; backend allowlist enforces actual support)
    return ['anthropic', 'openai', 'azure']
  }
  return [groupPlatform]
}
