package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// 202604231000_add_skill_market_backend_id
// Add `backend_id` to store the backend-assigned skill ID separately from the local auto-increment primary key.
// This avoids UNIQUE constraint conflicts when syncing from different backend environments.
// For existing data, id is migrated to backend_id (id was storing the backend id before this change).
func init() {
	Migrations.MustRegister(
		func(ctx context.Context, db *bun.DB) error {
			// Step 1: add the new column
			_, err := db.ExecContext(ctx, `
ALTER TABLE skill_market_skills
ADD COLUMN backend_id INTEGER;
`)
			if err != nil {
				return err
			}

			// Step 2: migrate existing id values to backend_id
			// Before this migration, the local id column stored the backend id.
			// After migration, id becomes a true auto-increment PK and backend_id stores the backend id.
			_, err = db.ExecContext(ctx, `
UPDATE skill_market_skills SET backend_id = id WHERE backend_id IS NULL;
`)
			return err
		},
		func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, `
ALTER TABLE skill_market_skills
DROP COLUMN backend_id;
`)
			return err
		},
	)
}
