-- v2 phase 4 / FR-69.1, FR-70.2: payments (the customer-facing gross billing
-- record) gains a currency. revenue_daily is redefined to group by currency
-- too — FR-70.2's rule that reports never sum across currencies applies to
-- this frozen view exactly as everywhere else; a caller wanting one number
-- must now explicitly pick a currency or build its own labeled conversion.

ALTER TABLE payments RENAME COLUMN amount_iqd TO amount;
ALTER TABLE payments ADD COLUMN currency text NOT NULL DEFAULT 'IQD' REFERENCES currencies(code);

DROP VIEW revenue_daily;
CREATE VIEW revenue_daily AS
    SELECT (at AT TIME ZONE 'Asia/Baghdad')::date AS date,
           source,
           currency,
           sum(amount)::bigint                  AS amount
      FROM payments
     GROUP BY 1, 2, 3;
