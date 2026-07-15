-- 0301_subscriber_language — Phase 4, Agent 3, range 0300–0309.
-- Contract C1-D / C2, FR-43. Per-subscriber language preference for the
-- portal (trilingual, Decision per sub-PRD 07); the login page's language
-- switcher writes here before a session exists, and PUT /portal/language
-- updates it afterward.
ALTER TABLE subscribers ADD COLUMN language text NOT NULL DEFAULT 'en';
