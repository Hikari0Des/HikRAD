package radius

// FR-144 setup wizard state persistence (migration 0610). One row per NAS;
// the panel PUTs this after every step transition (forward, Back, or a
// field edit) so gate items 3 (Back works everywhere) and 4 (resume after a
// browser close) are true of the SERVER's state, not merely client-side
// router history.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type wizardStateRow struct {
	Step      int
	FormData  json.RawMessage
	Completed bool
	UpdatedAt time.Time
	Found     bool
}

func getWizardState(ctx context.Context, db *pgxpool.Pool, nasID string) (wizardStateRow, error) {
	var s wizardStateRow
	err := db.QueryRow(ctx,
		`SELECT step, form_data, completed, updated_at FROM nas_setup_wizard_state WHERE nas_id = $1::uuid`, nasID).
		Scan(&s.Step, &s.FormData, &s.Completed, &s.UpdatedAt)
	if err == pgx.ErrNoRows {
		return wizardStateRow{Step: 1, FormData: json.RawMessage(`{}`)}, nil
	}
	if err != nil {
		return wizardStateRow{}, err
	}
	s.Found = true
	return s, nil
}

// saveWizardState upserts the whole row. form_data is written wholesale
// (never merged): the panel always sends the complete accumulated form —
// every step's values so far, including ones before the current step — so a
// Back-then-forward round trip can never lose a later step's entry by
// merging it away.
func saveWizardState(ctx context.Context, db *pgxpool.Pool, nasID string, step int, formData json.RawMessage, completed bool) error {
	_, err := db.Exec(ctx,
		`INSERT INTO nas_setup_wizard_state (nas_id, step, form_data, completed)
		 VALUES ($1::uuid, $2, $3, $4)
		 ON CONFLICT (nas_id) DO UPDATE SET
		    step = EXCLUDED.step, form_data = EXCLUDED.form_data, completed = EXCLUDED.completed, updated_at = now()`,
		nasID, step, formData, completed)
	return err
}
