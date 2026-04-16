package skillmarket

import (
	"context"
	"log"
	"strings"

	openclawskills "chatclaw/internal/openclaw/skills"
)

// ListCachedSkills 获取缓存技能列表（只返回 is_enabled=true 的，供前端使用）
// Wails 绑定方法
func (s *Service) ListCachedSkills(ctx context.Context, categoryID *int64, locale string) ([]Skill, error) {
	log.Printf("[skillmarket] ListCachedSkills called, locale=%s, categoryID=%v", locale, categoryID)
	syncSvc := NewSyncService()
	skills, err := syncSvc.GetCachedSkills(ctx, categoryID, locale)
	if err != nil {
		log.Printf("[skillmarket] ListCachedSkills: GetCachedSkills error: %v", err)
		return nil, err
	}
	log.Printf("[skillmarket] ListCachedSkills: returning %d cached skills", len(skills))
	// 转换为 Skill 类型
	result := make([]Skill, 0, len(skills))
	for _, skill := range skills {
		result = append(result, Skill{
			ID:           skill.ID,
			SkillName:    skill.SkillName,
			Name:         skill.Name,
			Description:  skill.Description,
			Instructions: skill.Instructions,
			IconURL:      skill.IconURL,
			CategoryID:   skill.CategoryID,
			CategoryName: skill.CategoryName,
			Source:       skill.Source,
			IsEnabled:    skill.IsEnabled,
			UpdatedAt:    skill.UpdatedAt,
		})
	}
	return result, nil
}

// ListCachedCategories 获取缓存分类列表
// Wails 绑定方法
func (s *Service) ListCachedCategories(ctx context.Context, locale string) ([]SkillCategory, error) {
	syncSvc := NewSyncService()
	categories, err := syncSvc.GetCachedCategories(ctx, locale)
	if err != nil {
		return nil, err
	}
	// 转换为 SkillCategory 类型
	result := make([]SkillCategory, 0, len(categories))
	for _, c := range categories {
		result = append(result, SkillCategory{
			ID:        c.ID,
			Name:      c.Name,
			NameLocal: c.Name,
			SortOrder: c.SortOrder,
			UpdatedAt: c.UpdatedAt,
		})
	}
	return result, nil
}

// CheckAndSyncSkillMarket 检查并同步技能市场缓存
// 返回是否有更新
// Wails 绑定方法
func (s *Service) CheckAndSyncSkillMarket(ctx context.Context, locale string) (bool, error) {
	log.Printf("[skillmarket] CheckAndSyncSkillMarket called, locale=%s", locale)
	syncSvc := NewSyncService()
	updated, err := syncSvc.CheckAndSync(ctx, locale)

	// 发送同步完成事件，通知前端刷新缓存数据
	s.app.Event.Emit("skillmarket:sync-completed", map[string]any{
		"locale":         locale,
		"updated":        updated,
		"error":          err != nil,
		"errorMessage":   "",
	})
	if err != nil {
		s.app.Event.Emit("skillmarket:sync-failed", map[string]any{
			"locale":       locale,
			"errorMessage": err.Error(),
		})
	}

	return updated, err
}

// GetCachedSkillByName 根据 skill_name 获取缓存技能
// Wails 绑定方法
func (s *Service) GetCachedSkillByName(ctx context.Context, skillName string, locale string) (*Skill, error) {
	syncSvc := NewSyncService()
	skill, err := syncSvc.GetCachedSkillByName(ctx, skillName, locale)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, nil
	}
	result := Skill{
		ID:           skill.ID,
		SkillName:    skill.SkillName,
		Name:         skill.Name,
		Description:  skill.Description,
		Instructions: skill.Instructions,
		IconURL:      skill.IconURL,
		CategoryID:   skill.CategoryID,
		CategoryName: skill.CategoryName,
		Source:       skill.Source,
		IsEnabled:    skill.IsEnabled,
		UpdatedAt:    skill.UpdatedAt,
	}
	return &result, nil
}

// InstalledSkillView 我的技能视图模型（前端合并后的展示类型）
type InstalledSkillView struct {
	// 本地技能原始字段
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Version     string `json:"version"`
	Eligible    *bool  `json:"eligible"`
	SkillRoot   string `json:"skillRoot"`
	Location    string `json:"location"`
	AgentID     *int64 `json:"agentId"`
	AgentName   string `json:"agentName"`
	DataSource  string `json:"dataSource"`

	// 缓存元数据
	RemoteID            int64   `json:"remoteId,omitempty"`
	RemoteSkillName    string  `json:"remoteSkillName,omitempty"`
	RemoteName         string  `json:"remoteName,omitempty"`
	RemoteDescription  string  `json:"remoteDescription,omitempty"`
	RemoteIconURL      string  `json:"remoteIconUrl,omitempty"`
	RemoteCategoryName string  `json:"remoteCategoryName,omitempty"`
	HasRemoteMeta      bool    `json:"hasRemoteMeta"`

	// 展示字段（优先使用缓存，否则使用本地）
	DisplayName        string `json:"displayName"`
	DisplayDescription string `json:"displayDescription"`
	DisplayIconURL    string `json:"displayIconUrl,omitempty"`

	// ScopeRoots maps scope string -> skill root directory path.
	// Key examples: "openclaw-shared", "local", "agent-workspace:main".
	// 用于前端"我的技能"中打开目录和读取文件时定位到当前 scope 对应的目录。
	ScopeRoots map[string]string `json:"scopeRoots"`
}

// MergeInstalledSkill 合并本地技能与缓存元数据
// Wails 绑定方法，供前端调用
func (s *Service) MergeInstalledSkill(local *openclawskills.OpenClawSkill, locale string) (*InstalledSkillView, error) {
	if local == nil {
		return nil, nil
	}

	view := &InstalledSkillView{
		Slug:        local.Slug,
		Name:        local.Name,
		Description: local.Description,
		Icon:        local.Icon,
		Version:     local.Version,
		Eligible:    local.Eligible,
		SkillRoot:   local.SkillRoot,
		Location:    local.Location,
		AgentID:     parseAgentID(local.AgentID),
		AgentName:   local.AgentName,
		DataSource:  local.DataSource,
	}

	// 查询缓存
	syncSvc := NewSyncService()
	ctx := context.Background()
	cached, err := syncSvc.GetCachedSkillByName(ctx, local.Slug, locale)
	if err == nil && cached != nil && cached.IsEnabled {
		view.RemoteID = cached.ID
		view.RemoteSkillName = cached.SkillName
		view.RemoteName = cached.Name
		view.RemoteDescription = cached.Description
		view.RemoteIconURL = cached.IconURL
		view.RemoteCategoryName = cached.CategoryName
		view.HasRemoteMeta = true
	}

	// 设置展示字段
	view.DisplayName = view.RemoteName
	if view.DisplayName == "" {
		view.DisplayName = view.Name
	}
	if view.DisplayName == "" {
		view.DisplayName = view.Slug
	}

	view.DisplayDescription = view.RemoteDescription
	if view.DisplayDescription == "" {
		view.DisplayDescription = view.Description
	}

	view.DisplayIconURL = view.RemoteIconURL

	// Build ScopeRoots: local SkillRoot 对应哪个 scope
	scope := "openclaw-shared"
	if local.Location == "workspace" && strings.TrimSpace(local.AgentID) != "" {
		scope = "agent-workspace:" + local.AgentID
	}
	if strings.TrimSpace(local.SkillRoot) != "" {
		view.ScopeRoots = map[string]string{scope: local.SkillRoot}
	}

	return view, nil
}

// MergeInstalledSkills 批量合并本地技能与缓存元数据
// Wails 绑定方法
func (s *Service) MergeInstalledSkills(locals []openclawskills.OpenClawSkill, locale string) ([]InstalledSkillView, error) {
	result := make([]InstalledSkillView, 0, len(locals))
	for i := range locals {
		view, err := s.MergeInstalledSkill(&locals[i], locale)
		if err != nil {
			return nil, err
		}
		if view != nil {
			result = append(result, *view)
		}
	}
	return result, nil
}

// parseAgentID converts string AgentID to *int64
func parseAgentID(agentID string) *int64 {
	if agentID == "" {
		return nil
	}
	var id int64
	for _, c := range agentID {
		if c >= '0' && c <= '9' {
			id = id*10 + int64(c-'0')
		} else {
			return nil
		}
	}
	return &id
}
