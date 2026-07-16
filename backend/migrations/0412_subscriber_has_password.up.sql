-- v1.1 item 13: passwordless hotspot subscribers. has_password mirrors whether
-- password_enc seals a non-empty credential (the ciphertext itself can't say —
-- AES-GCM of "" is still 29 opaque bytes). Every pre-existing row was created
-- under the password-required rule, so true is the correct backfill.
ALTER TABLE subscribers
    ADD COLUMN IF NOT EXISTS has_password boolean NOT NULL DEFAULT true;
