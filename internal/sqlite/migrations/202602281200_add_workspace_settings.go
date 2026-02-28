package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(
		func(ctx context.Context, db *bun.DB) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get user home dir: %w", err)
			}
			defaultWorkDir := filepath.Join(home, ".chatclaw")
			if err := os.MkdirAll(defaultWorkDir, 0o755); err != nil {
				return fmt.Errorf("create default work dir: %w", err)
			}

			sql := `
INSERT OR IGNORE INTO settings (key, value, type, category, description, created_at, updated_at) VALUES
  ('workspace_sandbox_mode', 'codex', 'string', 'workspace', 'Sandbox execution mode: codex or native', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT OR IGNORE INTO settings (key, value, type, category, description, created_at, updated_at) VALUES
  ('workspace_work_dir', ?, 'string', 'workspace', 'Working directory for sandbox execution', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
`
			_, err = db.ExecContext(ctx, sql, defaultWorkDir)
			return err
		},
		func(ctx context.Context, db *bun.DB) error {
			sql := `
DELETE FROM settings WHERE key IN ('workspace_sandbox_mode','workspace_work_dir');
`
			_, err := db.ExecContext(ctx, sql)
			return err
		},
	)
}
