-- v2-2 / FR-77.1 (contract C1): owner-managed named-provider catalog. No API
-- fields — a provider is a name and a transfer-instructions template a
-- subscriber reads, nothing more (NFR-7: no online dependency anywhere in
-- this feature). Edited in place: a provider's name/template is display
-- metadata, not a money-affecting figure like FR-71's cost or FR-74's
-- wholesale price, so there is no "what was the name at renewal time"
-- question to preserve — unlike profile_cost_history/reseller_prices, this
-- table is NOT append-only-versioned.

CREATE TABLE payment_providers (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  text NOT NULL,
    logo_path             text,
    instructions_template text NOT NULL DEFAULT '',
    enabled               boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now()
);
