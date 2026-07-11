/**
 * Feature switches for capabilities that land in a later phase but whose UI slot
 * exists now (task 4/handoff). Flip a flag when the backend ships; the Renew
 * button, for instance, activates when D delivers FR-19 in Phase 3.
 */
export const FEATURES = {
  /** Renew flow (FR-19) — live since Phase 3 (Agent 5). */
  renew: true,
} as const
