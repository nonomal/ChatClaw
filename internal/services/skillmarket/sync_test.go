package skillmarket

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func TestCheckSkillsUpdateUsesPageSizeAndRepairsCountMismatch(t *testing.T) {
	t.Parallel()

	db := newTestSkillMarketDB(t)
	t.Cleanup(func() { _ = db.Close() })

	now := "2026-04-23T10:30:00+08:00"
	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (id, skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?, ?)`,
		1, "alpha", "zh-CN", "Alpha", now, now,
	); err != nil {
		t.Fatalf("seed local skill: %v", err)
	}

	var fullSyncRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/skill/max-updated-at":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"maxUpdatedAt": now,
					"totalCount":   2,
				},
			})
		case "/skill/list":
			query := r.URL.Query()
			if got := query.Get("locale"); got != "zh-CN" {
				t.Fatalf("unexpected locale: %q", got)
			}
			if raw := query.Get("page_size"); raw != "" {
				t.Fatalf("unexpected snake_case page_size query: %q", raw)
			}
			if got := query.Get("page"); got != "1" {
				t.Fatalf("unexpected page for full sync: %q", got)
			}
			if got := query.Get("pageSize"); got != "9999" {
				t.Fatalf("unexpected pageSize for full sync: %q", got)
			}

			fullSyncRequests++
			items := []map[string]any{
				{
					"id":           1,
					"skillName":    "alpha",
					"name":         "Alpha",
					"description":  "alpha-desc",
					"instructions": "alpha-instructions",
					"iconUrl":      "https://example.com/a.png",
					"categoryName": "General",
					"source":       "chatclaw",
					"isEnabled":    true,
					"updatedAt":    now,
				},
			}
			if fullSyncRequests > 1 {
				items = append(items, map[string]any{
					"id":           2,
					"skillName":    "beta",
					"name":         "Beta",
					"description":  "beta-desc",
					"instructions": "beta-instructions",
					"iconUrl":      "https://example.com/b.png",
					"categoryName": "General",
					"source":       "chatclaw",
					"isEnabled":    true,
					"updatedAt":    now,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"items": items,
					"total": 2,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := &SyncService{
		db:         db,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		serverURL:  server.URL,
	}

	updated, err := svc.checkSkillsUpdate(context.Background(), "zh-CN")
	if err != nil {
		t.Fatalf("checkSkillsUpdate returned error: %v", err)
	}
	if !updated {
		t.Fatalf("expected count mismatch repair to report updated=true")
	}
	if fullSyncRequests != 2 {
		t.Fatalf("expected 2 full sync requests, got %d", fullSyncRequests)
	}

	count, err := svc.countLocalSkills(context.Background(), "zh-CN")
	if err != nil {
		t.Fatalf("countLocalSkills returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 local skills after repair sync, got %d", count)
	}
}

func TestFullSyncSkipsPreExistingIdConflict(t *testing.T) {
	t.Parallel()

	db := newTestSkillMarketDB(t)
	t.Cleanup(func() { _ = db.Close() })

	now := "2026-04-23T10:30:00+08:00"

	// 模拟跨环境场景：本地已有 id=1, skill_name="wechat"（来自旧环境的同步记录）
	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (id, skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?, ?)`,
		1, "wechat", "zh-CN", "OldWechat", now, now,
	); err != nil {
		t.Fatalf("seed local skill: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"items": []map[string]any{
					// 后端返回同一个 skill_name，但 id=99（不同的后端环境）
					{
						"id":           99,
						"skillName":    "wechat",
						"name":         "WeChat",
						"description":  "wechat-desc",
						"instructions": "wechat-instructions",
						"iconUrl":      "https://example.com/wx.png",
						"categoryName": "Communication",
						"source":       "chatclaw",
						"isEnabled":    true,
						"updatedAt":    now,
					},
				},
				"total": 1,
			},
		})
	}))
	defer server.Close()

	svc := &SyncService{
		db:         db,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		serverURL:  server.URL,
	}

	if err := svc.fullSyncSkills(context.Background(), "zh-CN"); err != nil {
		t.Fatalf("fullSyncSkills returned error (should handle id conflict gracefully): %v", err)
	}

	// 验证：只有一条记录，且 backend_id=99（来自后端），本地 id 保持自增
	count, err := svc.countLocalSkills(context.Background(), "zh-CN")
	if err != nil {
		t.Fatalf("countLocalSkills returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 local skill after fullSync, got %d", count)
	}

	var skill CachedSkill
	err = db.NewSelect().
		Table("skill_market_skills").
		Where("skill_name = ? AND locale = ?", "wechat", "zh-CN").
		Scan(context.Background(), &skill)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if skill.ID == 0 {
		t.Fatalf("expected local id to be auto-assigned (>0), got id=%d", skill.ID)
	}
	if skill.BackendID != 99 {
		t.Fatalf("expected backend_id=99 (from backend), got backend_id=%d", skill.BackendID)
	}
	if skill.Name != "WeChat" {
		t.Fatalf("expected skill name='WeChat', got %q", skill.Name)
	}
}

func TestFullSyncSoftDeletesSkills(t *testing.T) {
	t.Parallel()

	db := newTestSkillMarketDB(t)
	t.Cleanup(func() { _ = db.Close() })

	now := "2026-04-23T10:30:00+08:00"

	// 预先插入两个技能
	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?)`,
		"alpha", "zh-CN", "Alpha", now, now,
	); err != nil {
		t.Fatalf("seed skill alpha: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?)`,
		"beta", "zh-CN", "Beta", now, now,
	); err != nil {
		t.Fatalf("seed skill beta: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"items": []map[string]any{
					// 后端只返回 alpha，beta 已被删除
					{
						"id":           1,
						"skillName":    "alpha",
						"name":         "Alpha",
						"description":  "alpha-desc",
						"instructions": "alpha-instructions",
						"iconUrl":      "https://example.com/a.png",
						"categoryName": "General",
						"source":       "chatclaw",
						"isEnabled":    true,
						"updatedAt":    now,
					},
				},
				"total": 1,
			},
		})
	}))
	defer server.Close()

	svc := &SyncService{
		db:         db,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		serverURL:  server.URL,
	}

	if err := svc.fullSyncSkills(context.Background(), "zh-CN"); err != nil {
		t.Fatalf("fullSyncSkills returned error: %v", err)
	}

	// 验证：只有一条记录（beta 被清除了）
	count, err := svc.countLocalSkills(context.Background(), "zh-CN")
	if err != nil {
		t.Fatalf("countLocalSkills returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 local skill after fullSync (beta deleted), got %d", count)
	}
}

func TestIncrementalSyncSoftDeletesSkills(t *testing.T) {
	t.Parallel()

	db := newTestSkillMarketDB(t)
	t.Cleanup(func() { _ = db.Close() })

	now := "2026-04-23T10:30:00+08:00"
	old := "2026-04-23T09:00:00+08:00"
	deletedAt := "2026-04-23T10:00:00+08:00"

	// 预先插入一个技能
	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?)`,
		"beta", "zh-CN", "Beta", now, now,
	); err != nil {
		t.Fatalf("seed skill beta: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"items": []map[string]any{
					// 后端返回 beta 已被删除（deletedAt 不为空）
					{
						"id":           2,
						"skillName":    "beta",
						"name":         "Beta",
						"description":  "beta-desc",
						"instructions": "beta-instructions",
						"iconUrl":      "https://example.com/b.png",
						"categoryName": "General",
						"source":       "chatclaw",
						"isEnabled":    false,
						"updatedAt":    deletedAt,
						"deletedAt":    deletedAt,
					},
				},
			},
		})
	}))
	defer server.Close()

	svc := &SyncService{
		db:         db,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		serverURL:  server.URL,
	}

	if err := svc.incrementalSyncSkills(context.Background(), "zh-CN", old); err != nil {
		t.Fatalf("incrementalSyncSkills returned error: %v", err)
	}

	// 验证：技能被软删除，countLocalSkills 不包含它
	count, err := svc.countLocalSkills(context.Background(), "zh-CN")
	if err != nil {
		t.Fatalf("countLocalSkills returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 local skills after soft delete, got %d", count)
	}

	// 验证：技能记录存在但有 deleted_at
	var skill CachedSkill
	err = db.NewSelect().
		Table("skill_market_skills").
		Where("skill_name = ? AND locale = ?", "beta", "zh-CN").
		Scan(context.Background(), &skill)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if skill.DeletedAt == nil || *skill.DeletedAt == "" {
		t.Fatalf("expected deleted_at to be set, got nil")
	}
}

func TestIncrementalSyncSkipsPreExistingIdConflict(t *testing.T) {
	t.Parallel()

	db := newTestSkillMarketDB(t)
	t.Cleanup(func() { _ = db.Close() })

	now := "2026-04-23T10:30:00+08:00"
	old := "2026-04-23T09:00:00+08:00"

	if _, err := db.Exec(
		`INSERT INTO skill_market_skills (id, skill_name, locale, name, updated_at, synced_at) VALUES (?, ?, ?, ?, ?, ?)`,
		1, "wechat", "zh-CN", "OldWechat", old, old,
	); err != nil {
		t.Fatalf("seed local skill: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/skill/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"items": []map[string]any{
						{
							"id":           99,
							"skillName":    "wechat",
							"name":         "WeChat",
							"description":  "wechat-desc",
							"instructions": "wechat-instructions",
							"iconUrl":      "https://example.com/wx.png",
							"categoryName": "Communication",
							"source":       "chatclaw",
							"isEnabled":    true,
							"updatedAt":    now,
						},
					},
				},
			})
		}
	}))
	defer server.Close()

	svc := &SyncService{
		db:         db,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		serverURL:  server.URL,
	}

	if err := svc.incrementalSyncSkills(context.Background(), "zh-CN", old); err != nil {
		t.Fatalf("incrementalSyncSkills returned error (should handle id conflict gracefully): %v", err)
	}

	var skill CachedSkill
	err := db.NewSelect().
		Table("skill_market_skills").
		Where("skill_name = ? AND locale = ?", "wechat", "zh-CN").
		Scan(context.Background(), &skill)
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if skill.ID == 0 {
		t.Fatalf("expected local id to be auto-assigned (>0), got id=%d", skill.ID)
	}
	if skill.BackendID != 99 {
		t.Fatalf("expected backend_id=99, got backend_id=%d", skill.BackendID)
	}
	if skill.Name != "WeChat" {
		t.Fatalf("expected skill name='WeChat', got %q", skill.Name)
	}
}

func newTestSkillMarketDB(t *testing.T) *bun.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	db := bun.NewDB(sqlDB, sqlitedialect.New())
	stmts := []string{
		`CREATE TABLE skill_market_skills (
			id INTEGER PRIMARY KEY,
			backend_id INTEGER,
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
			deleted_at TEXT,
			UNIQUE(skill_name, locale)
		);`,
		`CREATE TABLE skill_market_sync_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("init test schema: %v", err)
		}
	}

	return db
}
