package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(
		func(ctx context.Context, db *bun.DB) error {
			sql := `
ALTER TABLE conversations ADD COLUMN conversation_source TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_conversations_agent_source_updated ON conversations(agent_id, conversation_source, updated_at DESC);
`
			if _, err := db.ExecContext(ctx, sql); err != nil {
				return err
			}
			return nil
		},
		func(ctx context.Context, db *bun.DB) error {
			sql := `
DROP INDEX IF EXISTS idx_conversations_agent_source_updated;
ALTER TABLE conversations DROP COLUMN conversation_source;
`
			if _, err := db.ExecContext(ctx, sql); err != nil {
				return err
			}
			return nil
		},
	)
}
