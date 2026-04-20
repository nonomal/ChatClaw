package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// 202604201813_add_model_default_use_model
// Add `default_use_model` to models so ChatWiki default model preference can be persisted locally.
func init() {
	Migrations.MustRegister(
		func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, `
ALTER TABLE models
ADD COLUMN default_use_model varchar(16) NOT NULL DEFAULT '0';
`)
			return err
		},
		func(ctx context.Context, db *bun.DB) error {
			_, err := db.ExecContext(ctx, `
ALTER TABLE models
DROP COLUMN default_use_model;
`)
			return err
		},
	)
}
