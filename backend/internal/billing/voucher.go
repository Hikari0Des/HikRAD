package billing

// Voucher generation + redemption (FR-22). Codes are crypto-random over an
// unambiguous alphabet (no 0/O/1/l/I), ≥ 10 chars including any prefix, and are
// stored only as a sha256 hash — plaintext exists solely in the generation
// response CSV. Charging is FROZEN at generation (C3): batch creation debits the
// creator's balance; void-batch credits the unused remainder back. Redemption is
// row-locked single-use and runs THE renewal path (source=voucher), so a
// double-redeem race can never double-apply.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/hikrad/hikrad/internal/radius"
	"github.com/jackc/pgx/v5"
)

// voucherAlphabet excludes visually ambiguous characters (FR-22.1).
const voucherAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// minCodeLen is the minimum total code length including prefix (FR-22.1);
// maxCodeLen caps what a batch may request (item 20: per-batch code length).
const (
	minCodeLen = 10
	maxCodeLen = 24
)

// randomPartLen is the number of random characters after the (upper-cased)
// prefix; kept ≥ 8 for entropy even with a long prefix. total ≤ 0 means the
// FR-22.1 minimum.
func randomPartLen(prefix string, total int) int {
	if total <= 0 {
		total = minCodeLen
	}
	n := total - len(prefix)
	if n < 8 {
		n = 8
	}
	return n
}

// genCode produces one plaintext voucher code with the (upper-cased) prefix.
func genCode(prefix string, total int) string {
	prefix = strings.ToUpper(prefix)
	n := randomPartLen(prefix, total)
	var b strings.Builder
	b.WriteString(prefix)
	max := big.NewInt(int64(len(voucherAlphabet)))
	for i := 0; i < n; i++ {
		idx, _ := rand.Int(rand.Reader, max)
		b.WriteByte(voucherAlphabet[idx.Int64()])
	}
	return b.String()
}

// normalizeCode canonicalizes a voucher code for hashing: upper-case, then
// drop every character outside [A-Z0-9]. Printed cards are often grouped
// ("ABCD-1234", "ABCD 1234") and subscribers type them exactly as printed —
// separators must never make a valid code read as invalid.
func normalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hashCode is the at-rest representation of a code (sha256 hex of the
// normalized code). Redemption hashes the submitted code and looks it up.
func hashCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}

// legacyHashCode is the pre-normalization at-rest form (upper-case + outer
// trim only). Vouchers issued before code normalization landed are stored
// under this hash; redemption falls back to it so printed stock stays valid.
func legacyHashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

// generateBatch creates a batch, debits the creator, and inserts count unique
// codes, returning the batch id and the plaintext codes (the only time they
// exist). Runs in one transaction so a balance debit without codes can't occur.
func (m *Module) generateBatch(ctx context.Context, in batchInput, creatorID string, enforce bool) (string, []string, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(ctx)

	// Resolve the profile price + currency (charge-at-generation basis) and
	// reject archived. v2 phase 4 / FR-69.4: the batch carries the generating
	// profile's currency; mechanism is otherwise unchanged from Phase 3.
	var unitPrice int64
	var unitCurrency string
	var archived bool
	err = tx.QueryRow(ctx, `SELECT price, currency, archived FROM profiles WHERE id = $1::uuid`, in.ProfileID).
		Scan(&unitPrice, &unitCurrency, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, errNoProfile
	}
	if err != nil {
		return "", nil, err
	}
	if archived {
		return "", nil, errProfileArchived
	}

	total := unitPrice * int64(in.Count)
	if creatorID != "" {
		bal, err := lockBalance(ctx, tx, creatorID, unitCurrency)
		if err != nil {
			return "", nil, err
		}
		if enforce && bal < total {
			return "", nil, errInsufficientFunds
		}
	}

	// Generation debit (charge at generation, C3). Balance-neutral for admins with
	// bypass off is impossible here — vouchers always debit the creator.
	genTx, err := insertLedger(ctx, tx, ledgerEntry{
		Type:           "adjustment",
		Amount:         -total,
		Currency:       unitCurrency,
		ActorManagerID: creatorID,
		Source:         "voucher",
		Note:           "voucher batch generation",
	})
	if err != nil {
		return "", nil, err
	}
	if creatorID != "" {
		if err := recomputeBalance(ctx, tx, creatorID, unitCurrency); err != nil {
			return "", nil, err
		}
	}

	var batchID string
	err = tx.QueryRow(ctx,
		`INSERT INTO voucher_batches
		   (profile_id, prefix, count, unit_price, currency, creator_manager_id, expires_at, gen_ledger_tx_id)
		 VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6,'')::uuid, $7, $8::uuid)
		 RETURNING id::text`,
		in.ProfileID, strings.ToUpper(in.Prefix), in.Count, unitPrice, unitCurrency, creatorID, in.ExpiresAt, genTx).
		Scan(&batchID)
	if err != nil {
		return "", nil, err
	}

	// Generate unique codes; the code_hash unique index is the backstop against a
	// (astronomically rare) cross-batch collision — top up any shortfall.
	plain, err := m.insertCodes(ctx, tx, batchID, in.Prefix, in.Count, in.CodeLength)
	if err != nil {
		return "", nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", nil, err
	}
	return batchID, plain, nil
}

// insertCodes inserts want unique codes for a batch and returns their plaintext.
func (m *Module) insertCodes(ctx context.Context, tx pgx.Tx, batchID, prefix string, want, codeLen int) ([]string, error) {
	plain := make([]string, 0, want)
	for attempt := 0; len(plain) < want && attempt < 5; attempt++ {
		need := want - len(plain)
		codes := make([]string, 0, need)
		hashes := make([]string, 0, need)
		seen := make(map[string]int, need)
		for len(codes) < need {
			c := genCode(prefix, codeLen)
			h := hashCode(c)
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = len(codes)
			codes = append(codes, c)
			hashes = append(hashes, h)
		}
		rows, err := tx.Query(ctx,
			`INSERT INTO vouchers (batch_id, code_hash)
			 SELECT $1::uuid, h FROM unnest($2::text[]) AS h
			 ON CONFLICT (code_hash) DO NOTHING
			 RETURNING code_hash`, batchID, hashes)
		if err != nil {
			return nil, err
		}
		inserted := map[string]struct{}{}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return nil, err
			}
			inserted[h] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i, h := range hashes {
			if _, ok := inserted[h]; ok {
				plain = append(plain, codes[i])
			}
		}
	}
	if len(plain) < want {
		return nil, errors.New("billing: could not generate enough unique voucher codes")
	}
	return plain, nil
}

// redeemResult reports a redemption outcome distinctly so the API and B's hotspot
// path can map it (invalid vs already-used vs expired vs ok).
type redeemOutcome int

const (
	redeemOK redeemOutcome = iota
	redeemInvalid
	redeemUsed
	redeemExpired
	redeemBatchVoid
)

// redeemVoucher validates + single-use-marks a code and runs the renewal for
// subscriberID inside one transaction (source=voucher; balance-neutral since the
// batch was charged at generation). Concurrent double-redeem is impossible: the
// voucher row is SELECT … FOR UPDATE'd, so the loser blocks then sees state=used.
func (m *Module) redeemVoucher(ctx context.Context, code, subscriberID, redeemerID string) (renewResult, redeemOutcome, error) {
	// Resolve settings BEFORE opening the transaction (pool-safety, see renewParams).
	rp := renewParams{
		subscriberID:   subscriberID,
		actorManagerID: redeemerID,
		source:         "voucher",
		ledgerType:     "voucher_redeem",
		method:         "voucher_redeem",
		note:           "voucher redemption",
		chargeBalance:  false,
		enforceBalance: false,
	}
	m.fillSettings(ctx, &rp)

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return renewResult{}, redeemInvalid, err
	}
	defer tx.Rollback(ctx)

	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
		return renewResult{}, redeemInvalid, err
	}
	dbNow = dbNow.UTC()

	var (
		voucherID  string
		vState     string
		batchID    string
		profileID  string
		batchState string
		expiresAt  *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT v.id::text, v.state, b.id::text, b.profile_id::text, b.state, b.expires_at
		   FROM vouchers v JOIN voucher_batches b ON b.id = v.batch_id
		  WHERE v.code_hash = ANY($1::text[])
		  FOR UPDATE OF v`, []string{hashCode(code), legacyHashCode(code)}).
		Scan(&voucherID, &vState, &batchID, &profileID, &batchState, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return renewResult{}, redeemInvalid, nil
	}
	if err != nil {
		return renewResult{}, redeemInvalid, err
	}
	if batchState == "void" || vState == "void" {
		return renewResult{}, redeemBatchVoid, nil
	}
	if vState == "used" {
		return renewResult{}, redeemUsed, nil
	}
	if expiresAt != nil && !expiresAt.After(dbNow) {
		return renewResult{}, redeemExpired, nil
	}

	// Run the renewal for the redeeming subscriber (balance-neutral, prepaid).
	rp.profileID = profileID
	rp.reference = voucherID
	res, err := m.renewInTx(ctx, tx, dbNow, rp)
	if err != nil {
		return renewResult{}, redeemInvalid, err
	}

	// Mark the voucher used (single-use). The FOR UPDATE lock above guarantees the
	// loser of a race sees this committed state=used and gets redeemUsed.
	if _, err := tx.Exec(ctx,
		`UPDATE vouchers
		    SET state = 'used', used_by_manager_id = NULLIF($2,'')::uuid,
		        used_for_subscriber_id = $3::uuid, used_at = now(), redeem_ledger_tx_id = $4::uuid
		  WHERE id = $1::uuid`,
		voucherID, redeemerID, subscriberID, res.LedgerTxID); err != nil {
		return renewResult{}, redeemInvalid, err
	}

	if err := tx.Commit(ctx); err != nil {
		return renewResult{}, redeemInvalid, err
	}

	// Post-commit side effects (same as a panel renewal).
	_ = radius.InvalidatePolicy(subscriberID)
	m.resetQuota(ctx, subscriberID)
	res.CoAResult = m.restoreCoA(ctx, subscriberID, res.rate, res.poolName)
	m.publishRenewed(ctx, subscriberID, res, "voucher")
	return res, redeemOK, nil
}
