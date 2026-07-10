-- 0102_subscribers_indexes — Phase 2, Agent 4, range 0100–0109.
-- Contract C7-D / FR-2 / AC-2a: global instant search across username/name/phone
-- must return in < 300 ms at 5 000 subscribers. Trigram GIN indexes serve the
-- substring (ILIKE '%q%') query. A fold function collapses Arabic orthographic
-- variants (hamza/alef forms, alef-maqsura, teh-marbuta, tatweel) so a name
-- typed with a different-but-equivalent spelling still matches (sub-PRD 04 §2).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- subscriber_fold normalizes Arabic variants to a canonical base form and
-- lower-cases. IMMUTABLE so it can back a functional index. Applied to both the
-- stored value (in the index) and the query term (at search time), so the two
-- always compare in the same normalized space.
CREATE OR REPLACE FUNCTION subscriber_fold(t text) RETURNS text AS $$
    SELECT lower(
        translate(
            COALESCE(t, ''),
            -- أ إ آ ٱ → ا ; ى → ي ; ة → ه ; ـ (tatweel) removed via ''-mapping
            'أإآٱىةـ',
            'اااايه'
        )
    );
$$ LANGUAGE sql IMMUTABLE;

-- Functional trigram indexes on the folded projections (username is citext, so
-- project to text first).
CREATE INDEX IF NOT EXISTS subscribers_username_fold_trgm_idx
    ON subscribers USING gin (subscriber_fold(username::text) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS subscribers_name_fold_trgm_idx
    ON subscribers USING gin (subscriber_fold(name) gin_trgm_ops);
-- Phone has no Arabic content but reuse the trigram index for substring match.
CREATE INDEX IF NOT EXISTS subscribers_phone_trgm_idx
    ON subscribers USING gin (phone gin_trgm_ops);
