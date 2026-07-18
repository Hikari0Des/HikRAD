-- v2-6 / FR-85.1 (contract C4): subscriber email, nullable, no uniqueness
-- constraint, no DB-level format CHECK — validated in Go alongside phone.
-- Not a credential (unlike the RADIUS password), so no encryption at rest.

ALTER TABLE subscribers ADD COLUMN email text;
