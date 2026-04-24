/**
 * Pending Registration Store
 *
 * Holds sensitive registration data (e.g. password) IN MEMORY ONLY while the
 * user navigates from /register to /email-verify. Intentionally not persisted
 * to localStorage / sessionStorage so the password never touches disk-backed
 * storage — see CodeQL js/clear-text-storage-of-sensitive-data.
 *
 * Data lives only for the duration of a page session; a hard refresh between
 * /register and /email-verify intentionally clears it, which forces the user
 * to start over (the desired fail-safe behavior).
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface PendingRegistrationData {
  email: string
  password: string
  turnstile_token?: string
  promo_code?: string
  invitation_code?: string
  aff_code?: string
  pending_auth_token?: string
  pending_auth_token_field?: 'pending_auth_token' | 'pending_oauth_token'
  pending_provider?: string
  pending_redirect?: string
  pending_adoption_decision?: {
    adopt_display_name?: boolean
    adopt_avatar?: boolean
  } | null
}

export const usePendingRegistrationStore = defineStore('pendingRegistration', () => {
  const data = ref<PendingRegistrationData | null>(null)

  function set(value: PendingRegistrationData): void {
    data.value = value
  }

  function get(): PendingRegistrationData | null {
    return data.value
  }

  function clear(): void {
    data.value = null
  }

  return { set, get, clear }
})
