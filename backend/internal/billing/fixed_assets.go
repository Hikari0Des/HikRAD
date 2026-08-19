package billing

// Fixed assets with straight-line depreciation (FR-157). Acquisition posts
// Dr Fixed Assets / Cr Cash; each month's depreciation run posts
// Dr Depreciation Expense / Cr Accumulated Depreciation, capped so the
// asset's accumulated depreciation never exceeds cost-salvage.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type createFixedAssetParams struct {
	Name             string
	Cost             int64
	AcquiredDate     time.Time
	UsefulLifeMonths int
	SalvageValue     int64
	CreatedBy        string
	Source           string
}

func createFixedAsset(ctx context.Context, tx pgx.Tx, at time.Time, p createFixedAssetParams) (string, error) {
	if p.Cost <= 0 || p.UsefulLifeMonths <= 0 {
		return "", errors.New("billing: fixed asset cost and useful life must be positive")
	}
	entryID, err := postJournal(ctx, tx, at, "acquire "+p.Name, p.Source, "", "", p.CreatedBy, []jLine{
		{AccountCode: acctFixedAssets, Debit: p.Cost, Memo: "acquire " + p.Name},
		{AccountCode: acctCash, Credit: p.Cost, Memo: "acquire " + p.Name},
	})
	if err != nil {
		return "", err
	}
	var assetID string
	err = tx.QueryRow(ctx,
		`INSERT INTO fixed_assets (name, cost, acquired_date, useful_life_months, salvage_value, acquisition_journal_entry_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6::uuid, NULLIF($7,'')::uuid) RETURNING id::text`,
		p.Name, p.Cost, p.AcquiredDate, p.UsefulLifeMonths, p.SalvageValue, entryID, p.CreatedBy).Scan(&assetID)
	return assetID, err
}

// runDepreciation posts one month's straight-line depreciation for assetID
// covering `period` (any date within the target month — normalized to the
// first of the month). A no-op (posted=false) if that month was already
// run (the depreciation_entries UNIQUE constraint) or the asset is fully
// depreciated.
func runDepreciation(ctx context.Context, tx pgx.Tx, at time.Time, assetID string, period time.Time, actorID, source string) (posted bool, err error) {
	periodStart := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)

	var cost, salvage int64
	var lifeMonths int
	err = tx.QueryRow(ctx, `SELECT cost, useful_life_months, salvage_value FROM fixed_assets WHERE id = $1::uuid`, assetID).
		Scan(&cost, &lifeMonths, &salvage)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("billing: fixed asset not found")
	}
	if err != nil {
		return false, err
	}

	var accumulated int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM depreciation_entries WHERE fixed_asset_id = $1::uuid`, assetID).
		Scan(&accumulated); err != nil {
		return false, err
	}
	depreciable := cost - salvage
	if accumulated >= depreciable {
		return false, nil // fully depreciated
	}
	monthly := depreciable / int64(lifeMonths)
	if monthly > depreciable-accumulated {
		monthly = depreciable - accumulated // final month rounds down to the exact remainder
	}
	if monthly <= 0 {
		return false, nil
	}

	entryID, err := postJournal(ctx, tx, at, "depreciation", source, "", "", actorID, []jLine{
		{AccountCode: acctDepreciationExpense, Debit: monthly, Memo: "depreciation"},
		{AccountCode: acctAccumDepreciation, Credit: monthly, Memo: "depreciation"},
	})
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO depreciation_entries (fixed_asset_id, period, amount, journal_entry_id) VALUES ($1::uuid, $2, $3, $4::uuid)`,
		assetID, periodStart, monthly, entryID)
	if isUniqueViolation(err) {
		return false, nil // this month was already run
	}
	return err == nil, err
}
