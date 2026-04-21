package chatwiki

import (
	"context"
	"database/sql"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

// newTestBunDB creates an in-memory SQLite database for testing.
func newTestBunDB(t *testing.T) *bun.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return bun.NewDB(sqlDB, sqlitedialect.New())
}

// setupTestTables creates the required tables for testing.
func setupTestTables(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()

	// Create chatwiki_bindings table
	_, err := db.ExecContext(ctx, `
CREATE TABLE chatwiki_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_url TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL,
    ttl INTEGER NOT NULL DEFAULT 0,
    exp INTEGER NOT NULL DEFAULT 0,
    user_id TEXT NOT NULL,
    user_name TEXT NOT NULL DEFAULT '',
    chatwiki_version TEXT NOT NULL DEFAULT 'dev',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);`)
	if err != nil {
		t.Fatalf("create chatwiki_bindings table: %v", err)
	}

	// Create models table for testing clearSyncedModelCatalogFromDB
	_, err = db.ExecContext(ctx, `
CREATE TABLE models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    name TEXT NOT NULL,
    model_type TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    default_use_model INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    capabilities TEXT NOT NULL,
    model_supplier TEXT NOT NULL,
    uni_model_name TEXT NOT NULL,
    price TEXT NOT NULL,
    region_scope TEXT NOT NULL,
    self_owned_model_config_id INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);`)
	if err != nil {
		t.Fatalf("create models table: %v", err)
	}

	// Create providers table with chatwiki entry
	_, err = db.ExecContext(ctx, `
CREATE TABLE providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 0,
    config TEXT,
    updated_at DATETIME NOT NULL
);`)
	if err != nil {
		t.Fatalf("create providers table: %v", err)
	}

	// Insert chatwiki provider (disabled by default)
	_, err = db.ExecContext(ctx, `
INSERT INTO providers (provider_id, enabled, updated_at) VALUES ('chatwiki', 0, datetime('now'));`)
	if err != nil {
		t.Fatalf("insert chatwiki provider: %v", err)
	}
}

// getProviderEnabled checks if the chatwiki provider is enabled.
func getProviderEnabled(t *testing.T, db *bun.DB) bool {
	t.Helper()
	ctx := context.Background()
	var enabled bool
	err := db.NewSelect().Table("providers").Column("enabled").Where("provider_id = 'chatwiki'").Scan(ctx, &enabled)
	if err != nil {
		t.Fatalf("get provider enabled: %v", err)
	}
	return enabled
}

// hasBinding checks if there's a binding in the database.
func hasBinding(t *testing.T, db *bun.DB) bool {
	t.Helper()
	ctx := context.Background()
	var count int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chatwiki_bindings").Scan(&count)
	if err != nil {
		t.Fatalf("has binding: %v", err)
	}
	return count > 0
}

// getBindingVersion returns the chatwiki_version of the binding.
func getBindingVersion(t *testing.T, db *bun.DB) string {
	t.Helper()
	ctx := context.Background()
	var version string
	err := db.NewSelect().Table("chatwiki_bindings").Column("chatwiki_version").Scan(ctx, &version)
	if err != nil {
		t.Fatalf("get binding version: %v", err)
	}
	return version
}

// hasChatWikiModels checks if there are models for the chatwiki provider.
func hasChatWikiModels(t *testing.T, db *bun.DB) bool {
	t.Helper()
	ctx := context.Background()
	var count int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM models WHERE provider_id = 'chatwiki'").Scan(&count)
	if err != nil {
		t.Fatalf("has chatwiki models: %v", err)
	}
	return count > 0
}

// TestSaveBindingCloudEnablesProvider verifies that cloud login enables the chatwiki provider.
func TestSaveBindingCloudEnablesProvider(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Ensure provider starts disabled
	if got := getProviderEnabled(t, db); got {
		t.Fatalf("provider should start disabled, got %v", got)
	}

	// Call the real SaveBinding function
	err := SaveBinding(nil,
		"https://cloud.example.com",
		"test-token",
		"3600",
		"9999999999",
		"user123",
		"Test User",
		"yun",
	)
	if err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Binding should exist
	if !hasBinding(t, db) {
		t.Fatal("binding should exist after SaveBinding")
	}

	// Provider should now be enabled
	if got := getProviderEnabled(t, db); !got {
		t.Fatal("provider should be enabled after cloud binding")
	}
}

// TestSaveBindingOpenSourceDisablesProvider verifies that open-source login disables the chatwiki provider.
func TestSaveBindingOpenSourceDisablesProvider(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// First enable the provider (simulating legacy state)
	ctx := context.Background()
	_, err := db.NewUpdate().Table("providers").Where("provider_id = 'chatwiki'").Set("enabled = ?", true).Exec(ctx)
	if err != nil {
		t.Fatalf("enable provider for test: %v", err)
	}

	// Verify provider starts enabled
	if got := getProviderEnabled(t, db); !got {
		t.Fatal("provider should start enabled for legacy state test")
	}

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Call the real SaveBinding function with open-source version
	err = SaveBinding(nil,
		"https://selfhosted.example.com",
		"test-token",
		"3600",
		"9999999999",
		"user456",
		"Test User 2",
		"dev",
	)
	if err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Binding should exist
	if !hasBinding(t, db) {
		t.Fatal("binding should exist after SaveBinding")
	}

	// Provider should now be disabled
	if got := getProviderEnabled(t, db); got {
		t.Fatal("provider should be disabled after open-source binding")
	}
}

// TestDeleteBindingDisablesProviderAndClearsModels verifies that delete disables provider and clears models.
func TestDeleteBindingDisablesProviderAndClearsModels(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// First save a binding and enable the provider
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
INSERT INTO chatwiki_bindings (server_url, token, ttl, exp, user_id, user_name, chatwiki_version, created_at, updated_at)
VALUES ('https://cloud.example.com', 'test-token', 3600, 9999999999, 'user789', 'Test User', 'yun', datetime('now'), datetime('now'));`)
	if err != nil {
		t.Fatalf("insert binding: %v", err)
	}

	// Enable provider
	_, err = db.NewUpdate().Table("providers").Where("provider_id = 'chatwiki'").Set("enabled = ?", true).Exec(ctx)
	if err != nil {
		t.Fatalf("enable provider: %v", err)
	}

	// Add some chatwiki models
	_, err = db.ExecContext(ctx, `
INSERT INTO models (provider_id, model_id, name, model_type, enabled, default_use_model, sort_order, capabilities, model_supplier, uni_model_name, price, region_scope, self_owned_model_config_id, created_at, updated_at)
VALUES ('chatwiki', 'model-1', 'Test Model', 'llm', 1, 0, 0, '[]', 'chatwiki', 'test-model', '', '', 0, datetime('now'), datetime('now'));`)
	if err != nil {
		t.Fatalf("insert model: %v", err)
	}

	// Verify initial state
	if !hasBinding(t, db) {
		t.Fatal("binding should exist before delete")
	}
	if !getProviderEnabled(t, db) {
		t.Fatal("provider should be enabled before delete")
	}
	if !hasChatWikiModels(t, db) {
		t.Fatal("models should exist before delete")
	}

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Create service and call DeleteBinding
	svc := NewChatWikiService(nil)
	err = svc.DeleteBinding()
	if err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}

	// Binding should be gone
	if hasBinding(t, db) {
		t.Fatal("binding should not exist after delete")
	}

	// Provider should be disabled
	if got := getProviderEnabled(t, db); got {
		t.Fatal("provider should be disabled after delete")
	}

	// Models should be cleared
	if hasChatWikiModels(t, db) {
		t.Fatal("models should be cleared after delete")
	}
}

// TestSaveBindingNormalizesVersion verifies that SaveBinding normalizes the version to lowercase.
func TestSaveBindingNormalizesVersion(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Call SaveBinding with uppercase version
	err := SaveBinding(nil,
		"https://cloud.example.com",
		"test-token",
		"3600",
		"9999999999",
		"user123",
		"Test User",
		"YUN", // uppercase
	)
	if err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Verify version is normalized to lowercase
	version := getBindingVersion(t, db)
	if version != "yun" {
		t.Fatalf("binding version should be normalized to 'yun', got %q", version)
	}
}

// TestSaveBindingUnknownVersionNormalization verifies that SaveBinding normalizes version to lowercase.
func TestSaveBindingUnknownVersionNormalization(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Call SaveBinding with uppercase version
	err := SaveBinding(nil,
		"https://cloud.example.com",
		"test-token",
		"3600",
		"9999999999",
		"user123",
		"Test User",
		"YUN", // uppercase
	)
	if err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Verify version is normalized to lowercase
	version := getBindingVersion(t, db)
	if version != "yun" {
		t.Fatalf("binding version should be normalized to 'yun', got %q", version)
	}
}

// TestSaveBindingEmptyVersionDefaultsToDev verifies that empty versions default to dev.
func TestSaveBindingEmptyVersionDefaultsToDev(t *testing.T) {
	db := newTestBunDB(t)
	setupTestTables(t, db)

	// Inject test DB
	origGetDB := getChatWikiServiceDB
	getChatWikiServiceDB = func() *bun.DB { return db }
	defer func() { getChatWikiServiceDB = origGetDB }()

	// Call SaveBinding with empty version
	err := SaveBinding(nil,
		"https://example.com",
		"test-token",
		"3600",
		"9999999999",
		"user999",
		"Test User",
		"", // empty
	)
	if err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Verify version defaults to dev
	version := getBindingVersion(t, db)
	if version != "dev" {
		t.Fatalf("binding version should default to 'dev', got %q", version)
	}

	// Provider should be disabled for empty/dev versions
	if got := getProviderEnabled(t, db); got {
		t.Fatal("provider should be disabled for empty/dev version")
	}
}
