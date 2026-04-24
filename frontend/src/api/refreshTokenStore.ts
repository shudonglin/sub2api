/**
 * Holds the OAuth refresh token in process memory only — never written
 * to localStorage / sessionStorage / cookies. An XSS payload that scrapes
 * web storage cannot exfiltrate this credential, and a page reload
 * forces a fresh login through the standard auth flow.
 *
 * Lives in its own module so both `api/auth.ts` (helper functions) and
 * `api/client.ts` (axios refresh interceptor) can read/write it without
 * a circular import.
 */

let inMemoryRefreshToken: string | null = null

export function setStoredRefreshToken(token: string | null): void {
  inMemoryRefreshToken = token
}

export function getStoredRefreshToken(): string | null {
  return inMemoryRefreshToken
}
