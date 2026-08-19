-- v4-9 (FR-153, Decision 63), sub-PRD 05. Range budgeted 0615-0622 in the
-- phase brief; the repo's real max at kickoff was already 0615 (v4-8's
-- coa_attempts, committed but not yet reflected in the brief when it was
-- written) — the master plan's "next free number, budgets are not
-- reservations" rule moves the whole range up by one, exactly the
-- renumbering v4-6/v4-7/v4-8 each needed before it. This phase's range is
-- 0616-0624.
--
-- Chart of accounts: the double-entry substrate's account catalog. Seeded
-- with a minimal default chart covering every account this phase's own
-- posting logic needs (journal.go's account code constants below) plus the
-- statement-facing cash/bank/capital/fixed-asset accounts FR-157 asks for.
-- Append-only in spirit (is_system rows are seed data, never edited by the
-- API — see accounts_api.go); user-added accounts (a manager's own bank
-- account, say) are ordinary rows with is_system=false.

CREATE TABLE accounts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code           text NOT NULL UNIQUE,
    name           text NOT NULL,
    type           text NOT NULL CHECK (type IN ('asset','liability','equity','revenue','expense')),
    -- subtype drives statement classification (which cash-flow bucket, which
    -- aging report, which balance-sheet subsection) without inventing a
    -- second taxonomy; NULL for a plain account with no special handling.
    subtype        text CHECK (subtype IS NULL OR subtype IN
                        ('cash','bank','accounts_receivable','accounts_payable',
                         'agent_wallet','fixed_asset','accumulated_depreciation',
                         'capital','suspense')),
    normal_balance text NOT NULL CHECK (normal_balance IN ('debit','credit')),
    is_system      boolean NOT NULL DEFAULT false,
    archived       boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX accounts_type_idx ON accounts (type);

-- Default chart (journal.go's acct* constants reference these codes by
-- string, never by id, so the seed is the single source of truth for the
-- codes this package's own posting logic depends on).
INSERT INTO accounts (code, name, type, subtype, normal_balance, is_system) VALUES
    ('1000', 'Cash',                       'asset',     'cash',                    'debit',  true),
    ('1010', 'Bank',                       'asset',     'bank',                    'debit',  true),
    ('1100', 'Accounts Receivable',        'asset',     'accounts_receivable',     'debit',  true),
    ('1500', 'Fixed Assets',               'asset',     'fixed_asset',             'debit',  true),
    ('1590', 'Accumulated Depreciation',   'asset',     'accumulated_depreciation','credit', true),
    ('2000', 'Accounts Payable',           'liability', 'accounts_payable',        'credit', true),
    -- Mirrors manager_balances (the agent/reseller prepaid wallet) on the
    -- owner's own books: the owner holds funds an agent has prepaid and owes
    -- them service in return — a liability, not owner cash.
    ('2100', 'Agent Prepaid Balances',     'liability', 'agent_wallet',            'credit', true),
    -- Matches a renewal's cost_at_sale (an allocation, not a cash event) so
    -- P&L can show accrual COGS without inventing a cash outflow that never
    -- happened; the overheads/expense screens are where the real bill lands.
    ('2900', 'Cost Accrual Suspense',      'liability', 'suspense',                'credit', true),
    ('3000', 'Owner Capital',              'equity',    'capital',                 'credit', true),
    ('3900', 'Retained Earnings',          'equity',    NULL,                      'credit', true),
    ('4000', 'Subscription Revenue',       'revenue',   NULL,                      'credit', true),
    ('4900', 'Other Revenue',              'revenue',   NULL,                      'credit', true),
    ('5000', 'Cost of Service',            'expense',   NULL,                      'debit',  true),
    ('5100', 'Commission Expense',         'expense',   NULL,                      'debit',  true),
    ('5200', 'Operating Expenses',         'expense',   NULL,                      'debit',  true),
    ('5900', 'Depreciation Expense',       'expense',   NULL,                      'debit',  true);
