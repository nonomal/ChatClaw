package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS skill_market_skills (
				id INTEGER PRIMARY KEY,
				skill_name TEXT NOT NULL,
				locale TEXT NOT NULL DEFAULT 'zh-CN',
				name TEXT NOT NULL DEFAULT '',
				description TEXT DEFAULT '',
				instructions TEXT DEFAULT '',
				icon_url TEXT DEFAULT '',
				category_id INTEGER,
				category_name TEXT DEFAULT '',
				source TEXT DEFAULT 'chatclaw',
				is_enabled INTEGER DEFAULT 1,
				updated_at TEXT NOT NULL,
				synced_at TEXT NOT NULL,
				UNIQUE(skill_name, locale)
			);
		`)
		if err != nil {
			return err
		}

		_, err = db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_skill_market_skills_skill_name_locale
			ON skill_market_skills(skill_name, locale);
		`)
		if err != nil {
			return err
		}

		_, err = db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_skill_market_skills_updated_at
			ON skill_market_skills(locale, updated_at);
		`)
		if err != nil {
			return err
		}

		_, err = db.Exec(`DROP TABLE IF EXISTS skill_market_categories;`)
		if err != nil {
			return err
		}
		_, err = db.Exec(`
			CREATE TABLE skill_market_categories (
				id INTEGER NOT NULL,
				locale TEXT NOT NULL DEFAULT 'zh-CN',
				name TEXT NOT NULL DEFAULT '',
				icon TEXT DEFAULT '',
				sort_order INTEGER DEFAULT 0,
				updated_at TEXT NOT NULL,
				synced_at TEXT NOT NULL,
				PRIMARY KEY (id, locale)
			);
		`)
		if err != nil {
			return err
		}

		_, err = db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_skill_market_categories_locale
			ON skill_market_categories(locale);
		`)
		if err != nil {
			return err
		}

		_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS skill_market_sync_meta (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL
			);
		`)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		db.Exec(`DROP TABLE IF EXISTS skill_market_skills;`)
		db.Exec(`DROP TABLE IF EXISTS skill_market_categories;`)
		db.Exec(`DROP TABLE IF EXISTS skill_market_sync_meta;`)
		return nil
	})
}
