-- Manager contact/profile fields (owner request 2026-07-19, item 16): the
-- managers table previously held only username/role/security columns, so a
-- manager had no full name, phone, email or address anywhere. All nullable —
-- absence is valid, nothing is backfilled.
ALTER TABLE managers
    ADD COLUMN IF NOT EXISTS full_name text,
    ADD COLUMN IF NOT EXISTS phone     text,
    ADD COLUMN IF NOT EXISTS email     text,
    ADD COLUMN IF NOT EXISTS address   text,
    ADD COLUMN IF NOT EXISTS notes     text;
