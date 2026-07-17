-- v2-2 follow-up (0582 has no backfill by design — "absence is off" per C3 —
-- but that default is wrong for an UPGRADE: pre-v2-2, voucher redemption had
-- no manager-level gate at all, and scratch-card payments were accepted
-- globally (card_payments.types was a single settings row, not per-manager).
-- Applying 0582's "no row = off" default retroactively to every EXISTING
-- manager would silently disable both payment methods for every subscriber
-- already relying on them the moment this migration runs — a real outage on
-- upgrade day, not a neutral default. Backfilling enabled=true here preserves
-- pre-migration behavior exactly; only a NEW manager created after this
-- migration (or an existing one explicitly opting out later) sees the
-- absence-is-off default in practice.

INSERT INTO manager_method_settings (manager_id, method_key, enabled)
SELECT id, 'voucher', true FROM managers
ON CONFLICT (manager_id, method_key) DO NOTHING;

INSERT INTO manager_method_settings (manager_id, method_key, enabled)
SELECT id, 'scratch_card', true FROM managers
ON CONFLICT (manager_id, method_key) DO NOTHING;
