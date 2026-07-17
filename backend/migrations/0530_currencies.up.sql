-- v2 phase 4 / FR-68.1 (contract C1): the currency catalog. Every monetary
-- integer column in the schema stores minor units of ITS ROW'S OWN currency;
-- IQD's minor_unit_digits=0 means its stored integer already IS the
-- whole-currency amount, which is exactly why the FR-68.2 renames (0531-0536)
-- change no IQD value. Adding a fourth currency later is a data-only INSERT +
-- a Settings toggle, never a schema change (mirrors FR-17's "no core changes
-- to add one more vendor" principle, applied to money).

CREATE TABLE currencies (
    code              text PRIMARY KEY,
    minor_unit_digits smallint NOT NULL,
    symbol            text NOT NULL DEFAULT '',
    enabled           boolean NOT NULL DEFAULT true
);

INSERT INTO currencies (code, minor_unit_digits, symbol) VALUES
    ('IQD', 0, 'د.ع'),
    ('USD', 2, '$'),
    ('EUR', 2, '€');
