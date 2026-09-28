/**
 * Connected-client auto-login (FR-167) + the captive-portal hand-off
 * (FR-168, the SAME endpoint with optional mac/ip hints — never a second
 * implementation) + the password step-up that upgrades a reduced-scope
 * session to full (FR-167).
 */
import { ApiError, request } from './client'
import type { LoginResponse } from './auth'

export interface AutoLoginHints {
  mac?: string
  ip?: string
}

/** A refusal here is a normal, silent, EXPECTED outcome for most page loads
 * (feature off, no live session, untrusted network, …) — callers must
 * never surface it as a user-facing error, only fall back to the login
 * screen quietly. Any failure at all (a 401 refusal, a network error, the
 * server being unreachable) means "no auto-login" — NFR-7's graceful-
 * degradation posture applies here as much as anywhere: this probe must
 * never be able to break page load, only skip its own feature. */
export async function tryAutoLogin(hints: AutoLoginHints = {}): Promise<LoginResponse | null> {
  try {
    return await request<LoginResponse>('/portal/session/auto', {
      body: { mac: hints.mac || undefined, ip: hints.ip || undefined },
      anonymous: true,
    })
  } catch {
    return null
  }
}

export type StepUpOutcome = { kind: 'ok'; response: LoginResponse } | { kind: 'invalid_password' }

/** FR-167: "the portal prompts, the subscriber types their password once,
 * and the session upgrades to full for its remaining lifetime." */
export async function stepUp(password: string): Promise<StepUpOutcome> {
  try {
    const res = await request<LoginResponse>('/portal/session/step-up', { body: { password } })
    return { kind: 'ok', response: res }
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return { kind: 'invalid_password' }
    throw err
  }
}
