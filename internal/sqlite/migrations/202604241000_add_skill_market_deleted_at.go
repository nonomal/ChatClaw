package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.ExecContext(ctx, `
			ALTER TABLE skill_market_skills
			ADD COLUMN deleted_at TEXT;
		`)
		if err != nil {
			return err
		}

		_, err = db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_skill_market_skills_deleted_at
			ON skill_market_skills(locale, deleted_at);
		`)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		db.Exec(`DROP INDEX IF EXISTS idx_skill_market_skills_deleted_at;`)
		db.Exec(`
			ALTER TABLE skill_market_skills
			DROP COLUMN deleted_at;
		`)
		return nil
	})
}
