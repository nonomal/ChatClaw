package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(
		func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, `
ALTER TABLE library ADD COLUMN batch_max_documents INTEGER NOT NULL DEFAULT 3;
ALTER TABLE library ADD COLUMN batch_max_chunks INTEGER NOT NULL DEFAULT 3;
`)
			return err
		},
		func(ctx context.Context, db *bun.DB) error {
			// SQLite < 3.35 cannot DROP COLUMN reliably; leave columns on rollback.
			_, _ = db.ExecContext(ctx, `SELECT 1`)
			return nil
		},
	)
}
