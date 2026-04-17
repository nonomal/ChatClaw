package skillmarket

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"chatclaw/internal/define"
	"chatclaw/internal/sqlite"

	"github.com/uptrace/bun"
)

// CachedSkill 缓存技能模型
type CachedSkill struct {
	ID            int64  `bun:"id" json:"id"`
	SkillName     string `bun:"skill_name" json:"skillName"`
	Locale        string `bun:"locale" json:"locale"`
	Name          string `bun:"name" json:"name"`
	Description   string `bun:"description" json:"description"`
	Instructions  string `bun:"instructions" json:"instructions"`
	IconURL       string `bun:"icon_url" json:"iconUrl"`
	CategoryID    *int64 `bun:"category_id" json:"categoryId"`
	CategoryName  string `bun:"category_name" json:"categoryName"`
	Source        string `bun:"source" json:"source"`
	IsEnabled     bool   `bun:"is_enabled" json:"isEnabled"`
	UpdatedAt     string `bun:"updated_at" json:"updatedAt"`
	SyncedAt      string `bun:"synced_at" json:"syncedAt"`
}

// CachedCategory 缓存分类模型
type CachedCategory struct {
	ID        int64  `bun:"id" json:"id"`
	Locale    string `bun:"locale" json:"locale"`
	Name      string `bun:"name" json:"name"`
	Icon      string `bun:"icon" json:"icon"`
	SortOrder int    `bun:"sort_order" json:"sortOrder"`
	UpdatedAt string `bun:"updated_at" json:"updatedAt"`
	SyncedAt  string `bun:"synced_at" json:"syncedAt"`
}

// SyncService 同步服务
type SyncService struct {
	db         *bun.DB
	httpClient *http.Client
	serverURL  string
}

// NewSyncService 创建同步服务
func NewSyncService() *SyncService {
	return &SyncService{
		db:         sqlite.DB(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
		serverURL:  strings.TrimSuffix(define.ServerURL, "/"),
	}
}

// getDB returns the DB instance (avoids field/method name conflict)
func (s *SyncService) getDB() *bun.DB {
	return s.db
}

// CheckAndSync 检查并同步，返回是否有更新
func (s *SyncService) CheckAndSync(ctx context.Context, locale string) (bool, error) {
	skillsNeedSync, err := s.checkSkillsUpdate(ctx, locale)
	if err != nil {
		return false, err
	}

	categoriesNeedSync, err := s.checkCategoriesUpdate(ctx, locale)
	if err != nil {
		return false, err
	}

	return skillsNeedSync || categoriesNeedSync, nil
}

// checkSkillsUpdate 检查技能更新
func (s *SyncService) checkSkillsUpdate(ctx context.Context, locale string) (bool, error) {
	localMaxAt, err := s.getLocalSkillsMaxUpdatedAt(locale)
	if err != nil || localMaxAt == "" {
		// 本地无数据，尝试全量同步（即使失败也不影响，因为本来就没数据）
		log.Printf("[skillmarket] checkSkillsUpdate: local empty, performing full sync (locale=%s)", locale)
		return true, s.fullSyncSkills(ctx, locale)
	}

	// 本地有数据，对比后端时间来判断是否需要增量同步
	remoteMaxAt, err := s.getRemoteSkillsMaxUpdatedAt(ctx)
	if err != nil {
		// 后端不可达，有本地数据则跳过同步（保留本地缓存）
		log.Printf("[skillmarket] checkSkillsUpdate: remote unavailable, skip sync (locale=%s): %v", locale, err)
		return false, nil
	}

	log.Printf("[skillmarket] checkSkillsUpdate: localMaxAt=%s, remoteMaxAt=%s (locale=%s)", localMaxAt, remoteMaxAt, locale)
	if remoteMaxAt == "" || !remoteMaxAtAfter(localMaxAt, remoteMaxAt) {
		return false, nil
	}

	return true, s.incrementalSyncSkills(ctx, locale, localMaxAt)
}

// checkCategoriesUpdate 检查分类更新
func (s *SyncService) checkCategoriesUpdate(ctx context.Context, locale string) (bool, error) {
	localMaxAt, err := s.getLocalCategoriesMaxUpdatedAt(locale)
	if err != nil || localMaxAt == "" {
		log.Printf("[skillmarket] checkCategoriesUpdate: local empty, performing full sync (locale=%s)", locale)
		return true, s.fullSyncCategories(ctx, locale)
	}

	remoteMaxAt, err := s.getRemoteCategoriesMaxUpdatedAt(ctx)
	if err != nil {
		// 对比时间请求失败，有本地数据则跳过同步，避免报错
		log.Printf("[skillmarket] checkCategoriesUpdate: remote unavailable, skip sync (locale=%s): %v", locale, err)
		return false, nil
	}

	if remoteMaxAt == "" || !remoteMaxAtAfter(localMaxAt, remoteMaxAt) {
		return false, nil
	}

	return true, s.incrementalSyncCategories(ctx, locale, localMaxAt)
}

// remoteMaxAtAfter 比较两个 RFC3339 时间字符串，a 是否在 b 之前
func remoteMaxAtAfter(a, b string) bool {
	if a == "" {
		return true
	}
	if b == "" {
		return false
	}
	ta, err1 := parseFlexibleTime(a)
	tb, err2 := parseFlexibleTime(b)
	if err1 != nil || err2 != nil {
		return strings.TrimSpace(a) < strings.TrimSpace(b)
	}
	return ta.Before(tb)
}

func parseFlexibleTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unsupported time format: %q", value)
	}
	return time.Time{}, lastErr
}

// getLocalSkillsMaxUpdatedAt 获取本地技能最大更新时间
func (s *SyncService) getLocalSkillsMaxUpdatedAt(locale string) (string, error) {
	var row struct {
		MaxUpdatedAt string `bun:"max_updated_at"`
	}
	err := s.getDB().NewSelect().
		ColumnExpr("MAX(updated_at) as max_updated_at").
		Where("locale = ?", locale).
		Table("skill_market_skills").
		Scan(context.Background(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.MaxUpdatedAt, nil
}

// getLocalCategoriesMaxUpdatedAt 获取本地分类最大更新时间
func (s *SyncService) getLocalCategoriesMaxUpdatedAt(locale string) (string, error) {
	var row struct {
		MaxUpdatedAt string `bun:"max_updated_at"`
	}
	err := s.getDB().NewSelect().
		ColumnExpr("MAX(updated_at) as max_updated_at").
		Where("locale = ?", locale).
		Table("skill_market_categories").
		Scan(context.Background(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.MaxUpdatedAt, nil
}

// getRemoteSkillsMaxUpdatedAt 获取后端技能最大更新时间
func (s *SyncService) getRemoteSkillsMaxUpdatedAt(ctx context.Context) (string, error) {
	reqURL := fmt.Sprintf("%s/skill/max-updated-at", s.serverURL)
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data struct {
			MaxUpdatedAt string `json:"maxUpdatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse max-updated-at response: %w", err)
	}
	return resp.Data.MaxUpdatedAt, nil
}

// getRemoteCategoriesMaxUpdatedAt 获取后端分类最大更新时间
func (s *SyncService) getRemoteCategoriesMaxUpdatedAt(ctx context.Context) (string, error) {
	reqURL := fmt.Sprintf("%s/skill-category/max-updated-at", s.serverURL)
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data struct {
			MaxUpdatedAt string `json:"maxUpdatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse max-updated-at response: %w", err)
	}
	return resp.Data.MaxUpdatedAt, nil
}

// fullSyncSkills 全量同步技能
func (s *SyncService) fullSyncSkills(ctx context.Context, locale string) error {
	reqURL := fmt.Sprintf("%s/skill/list?page=1&page_size=9999&locale=%s", s.serverURL, url.QueryEscape(locale))
	log.Printf("[skillmarket] fullSyncSkills: fetching %s", reqURL)
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		log.Printf("[skillmarket] fullSyncSkills: fetch failed: %v", err)
		return fmt.Errorf("fetch skills: %w", err)
	}

	var resp struct {
		Data struct {
			Items []struct {
				ID            int64  `json:"id"`
				SkillName     string `json:"skillName"`
				Name          string `json:"name"`
				Description   string `json:"description"`
				Instructions  string `json:"instructions"`
				IconURL       string `json:"iconUrl"`
				CategoryID    *int64 `json:"categoryId"`
				CategoryName  string `json:"categoryName"`
				Source        string `json:"source"`
				IsEnabled     bool   `json:"isEnabled"`
				UpdatedAt     string `json:"updatedAt"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse skills response: %w", err)
	}

	log.Printf("[skillmarket] fullSyncSkills: fetched %d items from backend", len(resp.Data.Items))

	now := sqlite.NowUTC()

	// 清空本地技能表（按语言）- 仅在请求成功后执行
	_, err = s.getDB().NewDelete().
		Table("skill_market_skills").
		Where("locale = ?", locale).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear local skills: %w", err)
	}

	// 写入全部数据
	for _, item := range resp.Data.Items {
		skill := CachedSkill{
			ID:           item.ID,
			SkillName:    item.SkillName,
			Locale:       locale,
			Name:         item.Name,
			Description:  item.Description,
			Instructions:  item.Instructions,
			IconURL:      item.IconURL,
			CategoryID:   item.CategoryID,
			CategoryName: item.CategoryName,
			Source:       item.Source,
			IsEnabled:    item.IsEnabled,
			UpdatedAt:    item.UpdatedAt,
			SyncedAt:     now,
		}
		_, err = s.getDB().ExecContext(ctx, `
			INSERT INTO skill_market_skills (id, skill_name, locale, name, description, instructions, icon_url, category_id, category_name, source, is_enabled, updated_at, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(skill_name, locale) DO UPDATE SET
				name = excluded.name,
				description = excluded.description,
				instructions = excluded.instructions,
				icon_url = excluded.icon_url,
				category_id = excluded.category_id,
				category_name = excluded.category_name,
				source = excluded.source,
				is_enabled = excluded.is_enabled,
				updated_at = excluded.updated_at,
				synced_at = excluded.synced_at
		`, skill.ID, skill.SkillName, skill.Locale, skill.Name, skill.Description, skill.Instructions, skill.IconURL, skill.CategoryID, skill.CategoryName, skill.Source, skill.IsEnabled, skill.UpdatedAt, skill.SyncedAt)
		if err != nil {
			return fmt.Errorf("insert skill %s: %w", item.SkillName, err)
		}
	}

	// 更新本地最大更新时间
	err = s.upsertSyncMeta(ctx, "skills_max_updated_at:"+locale, now)
	return err
}

// incrementalSyncSkills 增量同步技能
func (s *SyncService) incrementalSyncSkills(ctx context.Context, locale string, after string) error {
	reqURL := fmt.Sprintf("%s/skill/list?updated_after=%s&locale=%s", s.serverURL, url.QueryEscape(after), url.QueryEscape(locale))
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return fmt.Errorf("fetch incremental skills: %w", err)
	}

	var resp struct {
		Data struct {
			Items []struct {
				ID            int64  `json:"id"`
				SkillName     string `json:"skillName"`
				Name          string `json:"name"`
				Description   string `json:"description"`
				Instructions  string `json:"instructions"`
				IconURL       string `json:"iconUrl"`
				CategoryID    *int64 `json:"categoryId"`
				CategoryName  string `json:"categoryName"`
				Source        string `json:"source"`
				IsEnabled     bool   `json:"isEnabled"`
				UpdatedAt     string `json:"updatedAt"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse incremental skills response: %w", err)
	}

	now := sqlite.NowUTC()
	maxUpdatedAt := after

	for _, item := range resp.Data.Items {
		// 找出最新的 updated_at
		if item.UpdatedAt != "" && remoteMaxAtAfter(maxUpdatedAt, item.UpdatedAt) {
			maxUpdatedAt = item.UpdatedAt
		}

		if !item.IsEnabled {
			// 删除本地记录（禁用技能）
			_, _ = s.getDB().NewDelete().
				Table("skill_market_skills").
				Where("skill_name = ? AND locale = ?", item.SkillName, locale).
				Exec(ctx)
		} else {
			// 更新或插入
			skill := CachedSkill{
				ID:           item.ID,
				SkillName:    item.SkillName,
				Locale:       locale,
				Name:         item.Name,
				Description:  item.Description,
				Instructions: item.Instructions,
				IconURL:      item.IconURL,
				CategoryID:   item.CategoryID,
				CategoryName: item.CategoryName,
				Source:       item.Source,
				IsEnabled:    item.IsEnabled,
				UpdatedAt:    item.UpdatedAt,
				SyncedAt:     now,
			}
			_, err = s.getDB().ExecContext(ctx, `
				INSERT INTO skill_market_skills (id, skill_name, locale, name, description, instructions, icon_url, category_id, category_name, source, is_enabled, updated_at, synced_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(skill_name, locale) DO UPDATE SET
					name = excluded.name,
					description = excluded.description,
					instructions = excluded.instructions,
					icon_url = excluded.icon_url,
					category_id = excluded.category_id,
					category_name = excluded.category_name,
					source = excluded.source,
					is_enabled = excluded.is_enabled,
					updated_at = excluded.updated_at,
					synced_at = excluded.synced_at
			`, skill.ID, skill.SkillName, skill.Locale, skill.Name, skill.Description, skill.Instructions, skill.IconURL, skill.CategoryID, skill.CategoryName, skill.Source, skill.IsEnabled, skill.UpdatedAt, skill.SyncedAt)
			if err != nil {
				return fmt.Errorf("upsert skill %s: %w", item.SkillName, err)
			}
		}
	}

	// 更新本地最大更新时间
	if maxUpdatedAt != after {
		return s.upsertSyncMeta(ctx, "skills_max_updated_at:"+locale, maxUpdatedAt)
	}
	return nil
}

// fullSyncCategories 全量同步分类
func (s *SyncService) fullSyncCategories(ctx context.Context, locale string) error {
	reqURL := fmt.Sprintf("%s/skill-category/list?locale=%s", s.serverURL, url.QueryEscape(locale))
	log.Printf("[skillmarket] fullSyncCategories: fetching %s", reqURL)
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		log.Printf("[skillmarket] fullSyncCategories: fetch failed: %v", err)
		return fmt.Errorf("fetch categories: %w", err)
	}

	var resp struct {
		Data []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			NameLocal string `json:"nameLocal"`
			Icon      string `json:"icon"`
			SortOrder int    `json:"sortOrder"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse categories response: %w", err)
	}

	now := sqlite.NowUTC()

	// 清空本地分类表（按语言）
	_, err = s.getDB().NewDelete().
		Table("skill_market_categories").
		Where("locale = ?", locale).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("clear local categories: %w", err)
	}

	// 写入全部数据
	for _, item := range resp.Data {
		category := CachedCategory{
			ID:        item.ID,
			Locale:    locale,
			Name:      item.NameLocal,
			Icon:      item.Icon,
			SortOrder: item.SortOrder,
			UpdatedAt: item.UpdatedAt,
			SyncedAt:  now,
		}
		_, err = s.getDB().ExecContext(ctx, `
			INSERT INTO skill_market_categories (id, locale, name, icon, sort_order, updated_at, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id, locale) DO UPDATE SET
				name = excluded.name,
				icon = excluded.icon,
				sort_order = excluded.sort_order,
				updated_at = excluded.updated_at,
				synced_at = excluded.synced_at
		`, category.ID, category.Locale, category.Name, category.Icon, category.SortOrder, category.UpdatedAt, category.SyncedAt)
		if err != nil {
			return fmt.Errorf("insert category %d: %w", item.ID, err)
		}
	}

	// 更新本地最大更新时间
	err = s.upsertSyncMeta(ctx, "categories_max_updated_at:"+locale, now)
	return err
}

// incrementalSyncCategories 增量同步分类
func (s *SyncService) incrementalSyncCategories(ctx context.Context, locale string, after string) error {
	reqURL := fmt.Sprintf("%s/skill-category/list?updated_after=%s&locale=%s", s.serverURL, url.QueryEscape(after), url.QueryEscape(locale))
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return fmt.Errorf("fetch incremental categories: %w", err)
	}

	var resp struct {
		Data []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			NameLocal string `json:"nameLocal"`
			Icon      string `json:"icon"`
			SortOrder int    `json:"sortOrder"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse incremental categories response: %w", err)
	}

	now := sqlite.NowUTC()
	maxUpdatedAt := after

	for _, item := range resp.Data {
		if item.UpdatedAt != "" && remoteMaxAtAfter(maxUpdatedAt, item.UpdatedAt) {
			maxUpdatedAt = item.UpdatedAt
		}

		category := CachedCategory{
			ID:        item.ID,
			Locale:    locale,
			Name:      item.NameLocal,
			Icon:      item.Icon,
			SortOrder: item.SortOrder,
			UpdatedAt: item.UpdatedAt,
			SyncedAt:  now,
		}
		_, err = s.getDB().ExecContext(ctx, `
			INSERT INTO skill_market_categories (id, locale, name, icon, sort_order, updated_at, synced_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id, locale) DO UPDATE SET
				name = excluded.name,
				icon = excluded.icon,
				sort_order = excluded.sort_order,
				updated_at = excluded.updated_at,
				synced_at = excluded.synced_at
		`, category.ID, category.Locale, category.Name, category.Icon, category.SortOrder, category.UpdatedAt, category.SyncedAt)
		if err != nil {
			return fmt.Errorf("upsert category %d: %w", item.ID, err)
		}
	}

	if maxUpdatedAt != after {
		return s.upsertSyncMeta(ctx, "categories_max_updated_at:"+locale, maxUpdatedAt)
	}
	return nil
}

// upsertSyncMeta 写入同步元数据
func (s *SyncService) upsertSyncMeta(ctx context.Context, key, value string) error {
	_, err := s.getDB().ExecContext(ctx, `
		INSERT INTO skill_market_sync_meta (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// GetCachedSkills 获取缓存技能列表（只返回 is_enabled=true 的）
func (s *SyncService) GetCachedSkills(ctx context.Context, categoryID *int64, locale string) ([]CachedSkill, error) {
	query := s.getDB().NewSelect().
		Table("skill_market_skills").
		Where("locale = ? AND is_enabled = ?", locale, true).
		Order("id ASC")

	if categoryID != nil && *categoryID > 0 {
		query = query.Where("category_id = ?", *categoryID)
	}

	var skills []CachedSkill
	err := query.Scan(ctx, &skills)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return skills, nil
}

// GetCachedCategories 获取缓存分类列表
func (s *SyncService) GetCachedCategories(ctx context.Context, locale string) ([]CachedCategory, error) {
	var categories []CachedCategory
	err := s.getDB().NewSelect().
		Table("skill_market_categories").
		Where("locale = ?", locale).
		Order("sort_order ASC, id ASC").
		Scan(ctx, &categories)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return categories, nil
}

// GetCachedSkillByName 根据 skill_name 获取缓存技能
func (s *SyncService) GetCachedSkillByName(ctx context.Context, skillName, locale string) (*CachedSkill, error) {
	var skill CachedSkill
	err := s.getDB().NewSelect().
		Table("skill_market_skills").
		Where("skill_name = ? AND locale = ?", skillName, locale).
		Scan(ctx, &skill)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

// httpGet 发送 HTTP GET 请求
func (s *SyncService) httpGet(ctx context.Context, rawURL string) ([]byte, error) {
	const maxRetries = 3
	backoff := 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ChatClaw/1.0")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			if attempt < maxRetries {
				continue
			}
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			continue
		}

		var body []byte
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, fmt.Errorf("request failed after %d retries", maxRetries)
}

// min returns the minimum of two integers
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// StrToInt64 converts string to int64, returns 0 on error
func StrToInt64(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
