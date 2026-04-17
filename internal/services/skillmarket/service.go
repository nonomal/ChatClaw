package skillmarket

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"chatclaw/internal/define"
	"chatclaw/internal/openclaw/agents"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// InstallTargetScope identifies where a skill should be installed:
//   - "local": ChatClaw local skills directory (app data)
//   - "openclaw-shared": OpenClaw managed shared skills directory
//   - "agent-workspace:<openClawAgentID>": a specific OpenClaw agent's workspace skills directory
type InstallTargetScope string

const (
	ScopeLocal          InstallTargetScope = "local"
	ScopeOpenClawShared InstallTargetScope = "openclaw-shared"
)

// ScopeAgentWorkspacePrefix is the prefix for agent-workspace scopes.
const ScopeAgentWorkspacePrefix = "agent-workspace:"

// AgentWorkspaceScope returns the agent-workspace scope for a given OpenClaw agent ID.
func AgentWorkspaceScope(openClawAgentID string) InstallTargetScope {
	return InstallTargetScope(ScopeAgentWorkspacePrefix + openClawAgentID)
}

// ParseOpenClawAgentID parses the OpenClaw agent ID from an agent-workspace scope.
// Returns "" if the scope is not an agent-workspace scope.
func ParseOpenClawAgentID(scope InstallTargetScope) (string, bool) {
	if !strings.HasPrefix(string(scope), ScopeAgentWorkspacePrefix) {
		return "", false
	}
	return strings.TrimPrefix(string(scope), ScopeAgentWorkspacePrefix), true
}

type InstallTargetConfig struct {
	Scope           InstallTargetScope `json:"scope"`
	Path            string             `json:"path"`
	Label           string             `json:"label"`
	Available       bool               `json:"available"`
	OpenClawAgentID string             `json:"openClawAgentId,omitempty"`
}

type Service struct {
	app             *application.App
	httpClient      *http.Client
	openclawAgents  *openclawagents.OpenClawAgentsService
}

type targetState struct {
	root      string
	mu        sync.RWMutex
	installed map[string]bool
}

func NewService(app *application.App, openclawAgents *openclawagents.OpenClawAgentsService) *Service {
	return &Service{
		app:            app,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		openclawAgents: openclawAgents,
	}
}

func (s *Service) resolveLocalRoot() string {
	appDir, _ := define.AppDataDir()
	return filepath.Join(appDir, "skillmarket", "skills")
}

func (s *Service) resolveOpenClawSharedRoot() string {
	root, _ := define.OpenClawDataRootDir()
	return filepath.Join(root, "skills")
}

// parseOpenClawAgentsJSON reads and parses the agents section from openclaw.json.
func parseOpenClawAgentsJSON() map[string]string {
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, "openclaw.json"))
	if err != nil {
		return nil
	}
	var raw struct {
		Agents struct {
			List []struct {
				ID        string `json:"id"`
				Workspace string `json:"workspace"`
			} `json:"list"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, a := range raw.Agents.List {
		id := strings.TrimSpace(a.ID)
		if id != "" {
			result[id] = strings.TrimRight(strings.TrimSpace(a.Workspace), "/\\")
		}
	}
	return result
}

// resolveAgentWorkspaceRootByAgent returns the skills directory for a specific OpenClaw agent.
//
// Resolution priority:
//  1. work_dir field from openclaw_agents database table (if non-empty)
//     → work_dir + "\" + "workspace-" + openClawAgentID + "\skills"
//  2. workspace field from openclaw.json (if present)
//  3. <openclaw_root>/workspace-<openClawAgentID>/skills (fallback)
func (s *Service) resolveAgentWorkspaceRootByAgent(openClawAgentID string) string {
	// Priority 1: try openclaw.json first (it may have explicit workspace overrides)
	agents := parseOpenClawAgentsJSON()
	if agents != nil {
		if ws, ok := agents[openClawAgentID]; ok && ws != "" {
			return filepath.Join(ws, "skills")
		}
	}

	// Priority 2: read work_dir from openclaw_agents table
	allAgents, err := s.openclawAgents.ListAgents()
	if err == nil {
		for _, agent := range allAgents {
			if agent.OpenClawAgentID == openClawAgentID && agent.WorkDir != "" {
				return filepath.Join(
					strings.TrimRight(agent.WorkDir, "/\\"),
					"workspace-"+openClawAgentID,
					"skills",
				)
			}
		}
	}

	// Priority 3: fallback to openclaw_root hardcoded path
	root, _ := define.OpenClawDataRootDir()
	return filepath.Join(root, "workspace-"+openClawAgentID, "skills")
}

// agentWorkspaceRoots returns all agent workspace root directories.
// These are used to exclude agent workspaces from extraDirs scanning in openclaw-shared scope.
func (s *Service) agentWorkspaceRoots() map[string]bool {
	result := make(map[string]bool)

	// Read from openclaw.json
	agents := parseOpenClawAgentsJSON()
	if agents != nil {
		for _, ws := range agents {
			if ws != "" {
				result[ws] = true
			}
		}
	}

	// Also read from database openclaw_agents table (work_dir field)
	allAgents, err := s.openclawAgents.ListAgents()
	if err == nil {
		for _, agent := range allAgents {
			if agent.WorkDir != "" {
				ws := filepath.Join(
					strings.TrimRight(agent.WorkDir, "/\\"),
					"workspace-"+agent.OpenClawAgentID,
				)
				result[ws] = true
			}
		}
	}

	return result
}

func (s *Service) getTargetStateForScope(scope InstallTargetScope) (*targetState, error) {
	var root string
	switch scope {
	case ScopeLocal:
		root = s.resolveLocalRoot()
	case ScopeOpenClawShared:
		root = s.resolveOpenClawSharedRoot()
	default:
		openClawAgentID, ok := ParseOpenClawAgentID(scope)
		if !ok {
			return nil, fmt.Errorf("invalid agent workspace scope: %s", scope)
		}
		root = s.resolveAgentWorkspaceRootByAgent(openClawAgentID)
	}

	st := &targetState{
		root:      root,
		installed: make(map[string]bool),
	}
	_ = os.MkdirAll(root, 0o755)
	return st, nil
}

func (s *Service) ListAvailableTargets(agentID *int64, locale string) ([]InstallTargetConfig, error) {
	if agentID != nil && *agentID > 0 {
		return s.listTargetsForAgent(*agentID)
	}
	return s.listGlobalTargets(locale)
}

func (s *Service) listGlobalTargets(locale string) ([]InstallTargetConfig, error) {
	configs := []InstallTargetConfig{
		{
			Scope:     ScopeLocal,
			Path:      s.resolveLocalRoot(),
			Label:     "本地目录",
			Available: true,
		},
		{
			Scope:     ScopeOpenClawShared,
			Path:      s.resolveOpenClawSharedRoot(),
			Label:     "OpenClaw 共享技能",
			Available: true,
		},
	}
	// Ensure dirs exist
	for i := range configs {
		if err := os.MkdirAll(configs[i].Path, 0o755); err != nil {
			configs[i].Available = false
		}
	}
	return configs, nil
}

func (s *Service) listTargetsForAgent(agentID int64) ([]InstallTargetConfig, error) {
	// Resolve OpenClaw agent ID from database ID
	agent, err := s.openclawAgents.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent: %w", err)
	}
	openClawAgentID := agent.OpenClawAgentID

	st, err := s.getTargetStateForScope(AgentWorkspaceScope(openClawAgentID))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent workspace: %w", err)
	}
	configs := []InstallTargetConfig{
		{
			Scope:           AgentWorkspaceScope(openClawAgentID),
			Path:            st.root,
			Label:           fmt.Sprintf("%s 工作目录", agent.Name),
			Available:       true,
			OpenClawAgentID: openClawAgentID,
		},
		{
			Scope:     ScopeOpenClawShared,
			Path:      s.resolveOpenClawSharedRoot(),
			Label:     "OpenClaw 共享技能",
			Available: true,
		},
	}
	// Ensure dirs exist
	for i := range configs {
		if err := os.MkdirAll(configs[i].Path, 0o755); err != nil {
			configs[i].Available = false
		}
	}
	return configs, nil
}

type AgentWithTargets struct {
	Agent   openclawagents.OpenClawAgent   `json:"agent"`
	Targets []InstallTargetConfig          `json:"targets"`
}

// ListAgentsWithTargets returns all agents with their workspace targets and shared targets in one call.
// This eliminates the race condition in the frontend when switching agents.
func (s *Service) ListAgentsWithTargets(locale string) ([]AgentWithTargets, []InstallTargetConfig, error) {
	agents, err := s.openclawAgents.ListAgents()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list agents: %w", err)
	}

	sharedTargets, err := s.listGlobalTargets(locale)
	if err != nil {
		return nil, nil, err
	}

	var agentTargets []AgentWithTargets
	for _, agent := range agents {
		st, err := s.getTargetStateForScope(AgentWorkspaceScope(agent.OpenClawAgentID))
		if err != nil {
			continue
		}
		// Ensure dir exists
		_ = os.MkdirAll(st.root, 0o755)

		openClawSharedTarget := InstallTargetConfig{
			Scope:     ScopeOpenClawShared,
			Path:      s.resolveOpenClawSharedRoot(),
			Label:     "OpenClaw 共享技能",
			Available: true,
		}
		_ = os.MkdirAll(openClawSharedTarget.Path, 0o755)

		at := AgentWithTargets{
			Agent: agent,
			Targets: []InstallTargetConfig{
				{
					Scope:           AgentWorkspaceScope(agent.OpenClawAgentID),
					Path:            st.root,
					Label:           fmt.Sprintf("%s 工作目录", agent.Name),
					Available:       true,
					OpenClawAgentID: agent.OpenClawAgentID,
				},
				openClawSharedTarget,
			},
		}
		agentTargets = append(agentTargets, at)
	}

	return agentTargets, sharedTargets, nil
}

func (s *Service) getTargetState(scope InstallTargetScope) (*targetState, error) {
	return s.getTargetStateForScope(scope)
}

type SkillCategory struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	NameLocal  string            `json:"nameLocal"`
	NameI18n   map[string]string `json:"nameI18n,omitempty"`
	SortOrder  int               `json:"sortOrder"`
	SkillCount int               `json:"skillCount"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
}

type Skill struct {
	ID           int64  `json:"id"`
	CategoryID   *int64 `json:"categoryId"`
	CategoryName string `json:"categoryName,omitempty"`
	IsBuiltin    bool   `json:"isBuiltin"`
	IsEnabled    bool   `json:"isEnabled"`
	SkillName    string `json:"skillName"`
	IconURL      string `json:"iconUrl"`
	DownloadType string `json:"downloadType"`
	Source       string `json:"source"`
	OssKey       string `json:"ossKey,omitempty"`
	SortOrder    int    `json:"sortOrder"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type SkillDetail struct {
	Skill
	I18n map[string]SkillI18nItem `json:"i18n,omitempty"`
}

type SkillI18nItem struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

func (s *Service) ListCategories(ctx context.Context, locale string) ([]SkillCategory, error) {
	baseURL := strings.TrimSuffix(define.ServerURL, "/")
	reqURL := fmt.Sprintf("%s/skill-category/list?locale=%s", baseURL, url.QueryEscape(locale))
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []SkillCategory `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse categories response: %w", err)
	}
	return resp.Data, nil
}

type ListSkillsParams struct {
	CategoryID *int64             `json:"categoryId,omitempty"`
	Name       string             `json:"name,omitempty"`
	Locale     string             `json:"locale,omitempty"`
	Page       int                `json:"page,omitempty"`
	PageSize   int                `json:"pageSize,omitempty"`
	Scope      InstallTargetScope `json:"scope,omitempty"`
}

func (s *Service) ListSkills(ctx context.Context, params ListSkillsParams) ([]Skill, int64, error) {
	baseURL := strings.TrimSuffix(define.ServerURL, "/")

	q := url.Values{}
	if params.CategoryID != nil {
		q.Set("categoryId", strconv.FormatInt(*params.CategoryID, 10))
	}
	if params.Name != "" {
		q.Set("name", params.Name)
	}
	if params.Locale != "" {
		q.Set("locale", params.Locale)
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(params.PageSize))
	}

	reqURL := fmt.Sprintf("%s/skill/list?%s", baseURL, q.Encode())
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Data struct {
			Items []Skill `json:"items"`
			Total int64   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse skills response: %w", err)
	}

	total := resp.Data.Total
	skills := resp.Data.Items

	// Check installed status based on current scope
	if params.Scope != "" {
		st, err := s.getTargetStateForScope(params.Scope)
		if err == nil {
			s.refreshInstalledCacheForScope(params.Scope, st)
			st.mu.RLock()
			for i := range skills {
				if st.installed[skills[i].SkillName] {
					skills[i].IsBuiltin = true
				}
			}
			st.mu.RUnlock()
		}
	}

	return skills, total, nil
}

func (s *Service) GetSkillDetail(ctx context.Context, id int64, locale string) (*SkillDetail, error) {
	baseURL := strings.TrimSuffix(define.ServerURL, "/")

	reqURL := fmt.Sprintf("%s/skill/%d?locale=%s", baseURL, id, url.QueryEscape(locale))
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data SkillDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse skill detail response: %w", err)
	}
	return &resp.Data, nil
}

func (s *Service) GetSkillDownloadURL(ctx context.Context, id int64) (string, error) {
	baseURL := strings.TrimSuffix(define.ServerURL, "/")

	reqURL := fmt.Sprintf("%s/skill/download-url/%d", baseURL, id)
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse download URL response: %w", err)
	}
	return resp.Data.URL, nil
}

func (s *Service) GetInstalledSkillNames(scope InstallTargetScope) ([]string, error) {
	st, err := s.getTargetStateForScope(scope)
	if err != nil {
		return nil, fmt.Errorf("install target not available: %s", scope)
	}

	s.refreshInstalledCacheForScope(scope, st)

	st.mu.RLock()
	defer st.mu.RUnlock()

	names := make([]string, 0, len(st.installed))
	for name := range st.installed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Service) IsSkillInstalled(skillName string, scope InstallTargetScope) bool {
	st, err := s.getTargetStateForScope(scope)
	if err != nil {
		return false
	}
	s.refreshInstalledCacheForScope(scope, st)
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.installed[skillName]
}

func (s *Service) IsSkillInstalledInAny(skillName string) bool {
	// Scan: local + openclaw-shared + known agent workspaces
	roots := []string{s.resolveLocalRoot(), s.resolveOpenClawSharedRoot()}
	for _, root := range roots {
		if s.hasSkillUnlocked(root, skillName) {
			return true
		}
	}
	return false
}

func (s *Service) hasSkillUnlocked(skillRoot, skillName string) bool {
	skillDir := filepath.Join(skillRoot, skillName)
	for _, marker := range []string{"SKILL.md", "config.json"} {
		if _, err := os.Stat(filepath.Join(skillDir, marker)); err == nil {
			return true
		}
	}
	return false
}

func (s *Service) InstallSkill(ctx context.Context, skill Skill, scope InstallTargetScope) error {
	st, err := s.getTargetStateForScope(scope)
	if err != nil {
		return fmt.Errorf("install target not available: %s", scope)
	}

	switch strings.ToLower(skill.Source) {
	case "clawhub":
		return s.installFromClawhub(ctx, skill, st)
	case "skillhub":
		return s.installFromSkillhub(ctx, skill, st)
	case "chatclaw":
		return s.installFromChatclaw(ctx, skill, st)
	default:
		return fmt.Errorf("unknown skill source: %s", skill.Source)
	}
}

func (s *Service) UninstallSkill(ctx context.Context, skillName string, scope InstallTargetScope) error {
	st, err := s.getTargetStateForScope(scope)
	if err != nil {
		return fmt.Errorf("install target not available: %s", scope)
	}

	targetDirs, err := s.resolveInstalledSkillDirs(skillName, scope)
	if err != nil {
		return err
	}
	if len(targetDirs) == 0 {
		return fmt.Errorf("skill directory not found for %q in scope %s", skillName, scope)
	}

	for _, targetDir := range targetDirs {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("remove skill directory %s: %w", targetDir, err)
		}
	}

	st.mu.Lock()
	delete(st.installed, skillName)
	st.mu.Unlock()
	return nil
}

func (s *Service) resolveInstalledSkillDirs(skillName string, scope InstallTargetScope) ([]string, error) {
	roots, err := s.uninstallRootsForScope(scope)
	if err != nil {
		return nil, err
	}

	var dirs []string
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		targetDir, err := s.validateSkillDir(skillName, root)
		if err != nil {
			return nil, err
		}
		info, statErr := os.Stat(targetDir)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat skill directory %s: %w", targetDir, statErr)
		}
		if !info.IsDir() {
			continue
		}
		cleaned := filepath.Clean(targetDir)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		dirs = append(dirs, cleaned)
	}
	return dirs, nil
}

func (s *Service) uninstallRootsForScope(scope InstallTargetScope) ([]string, error) {
	switch scope {
	case ScopeLocal:
		return []string{s.resolveLocalRoot()}, nil
	case ScopeOpenClawShared:
		roots := []string{s.resolveOpenClawSharedRoot()}
		ocRoot, err := define.OpenClawDataRootDir()
		if err != nil {
			return roots, nil
		}
		extraDirs := readOpenClawExtraDirs(filepath.Join(ocRoot, "openclaw.json"))
		if len(extraDirs) > 0 {
			for _, dir := range extraDirs {
				if abs := expandExtraDirPath(dir); abs != "" {
					roots = append(roots, abs)
				}
			}
		} else if rtRoot, err := resolveRuntimeRoot(); err == nil {
			roots = append(roots, filepath.Join(rtRoot, "extraSkills"))
		}
		return dedupePaths(roots), nil
	default:
		openClawAgentID, ok := ParseOpenClawAgentID(scope)
		if !ok {
			return nil, fmt.Errorf("invalid agent workspace scope: %s", scope)
		}
		return []string{s.resolveAgentWorkspaceRootByAgent(openClawAgentID)}, nil
	}
}

func dedupePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		cleaned := filepath.Clean(p)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func (s *Service) installFromClawhub(ctx context.Context, skill Skill, st *targetState) error {
	const (
		primaryBase = "https://clawhub.ai/api/v1"
		mirrorBase  = "https://cn.clawhub-mirror.com/api/v1"
	)
	slug := skill.SkillName

	var metaResp struct {
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}

	baseURL := s.clawhubBaseURLWithFallback(ctx, primaryBase, mirrorBase)
	body, err := s.fetchClawhubSkillMeta(ctx, baseURL, slug)
	if err != nil && baseURL == primaryBase {
		body, err = s.fetchClawhubSkillMeta(ctx, mirrorBase, slug)
		if err == nil {
			baseURL = mirrorBase
		}
	}
	if err != nil {
		return fmt.Errorf("fetch clawhub skill metadata: %w", err)
	}
	if err := json.Unmarshal(body, &metaResp); err != nil {
		return fmt.Errorf("parse clawhub meta: %w", err)
	}

	if metaResp.LatestVersion == nil || metaResp.LatestVersion.Version == "" {
		return fmt.Errorf("cannot resolve version for %s", slug)
	}
	version := metaResp.LatestVersion.Version

	if err := s.installClawhubZip(ctx, slug, version, st, baseURL); err != nil {
		if baseURL == primaryBase {
			if mirrorErr := s.installClawhubZip(ctx, slug, version, st, mirrorBase); mirrorErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

func (s *Service) fetchClawhubSkillMeta(ctx context.Context, baseURL, slug string) ([]byte, error) {
	metaURL := fmt.Sprintf("%s/skills/%s", baseURL, url.PathEscape(slug))
	return s.httpGetWithTimeout(ctx, metaURL, 10*time.Second)
}

func (s *Service) installClawhubZip(ctx context.Context, slug, version string, st *targetState, baseURL string) error {
	params := url.Values{}
	params.Set("slug", slug)
	params.Set("version", version)
	zipURL := fmt.Sprintf("%s/download?%s", baseURL, params.Encode())

	tmpDir, err := os.MkdirTemp("", "clawhub-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, slug+".zip")
	if err := s.downloadFileWithTimeout(ctx, zipURL, zipPath, 20*time.Second); err != nil {
		return fmt.Errorf("download clawhub zip: %w", err)
	}

	stageDir := filepath.Join(tmpDir, "stage")
	if err := extractZip(zipPath, stageDir); err != nil {
		return fmt.Errorf("extract clawhub zip: %w", err)
	}
	if !isValidSkillDir(stageDir) {
		return fmt.Errorf("downloaded clawhub zip for %s did not contain a valid skill", slug)
	}

	targetDir := filepath.Join(st.root, slug)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		backupDir := filepath.Join(st.root,
			fmt.Sprintf(".backup-%s-%d", sanitizePathPart(slug), time.Now().UnixNano()))
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("backup existing: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(backupDir)
		}()
	}

	if err := os.Rename(stageDir, targetDir); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}

	st.mu.Lock()
	st.installed[slug] = true
	st.mu.Unlock()
	return nil
}

// clawhubBaseURLWithFallback probes primary with a short timeout.
// Returns primaryBase on success, otherwise falls back to mirrorBase.
func (s *Service) clawhubBaseURLWithFallback(ctx context.Context, primaryBase, mirrorBase string) string {
	const probeTimeout = 8 * time.Second
	probeURL := fmt.Sprintf("%s/skills/clawhub-probe", primaryBase)
	if _, err := s.httpGetWithTimeout(ctx, probeURL, probeTimeout); err == nil {
		return primaryBase
	}
	// Primary unreachable or timed out; use mirror.
	return mirrorBase
}

// isTimeoutErr reports whether err is likely a context timeout.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "Handshake did not verify") ||
		strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "no such host")
}

func (s *Service) installFromSkillhub(ctx context.Context, skill Skill, st *targetState) error {
	const (
		indexFallback   = "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills.json"
		downloadTpl     = "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills/{slug}.zip"
		primaryFallback = "http://lightmake.site/api/v1/download?slug={slug}"
	)
	slug := skill.SkillName

	var zipURL string
	data, err := s.httpGet(ctx, indexFallback)
	if err == nil {
		var index struct {
			Skills []struct {
				Slug   string `json:"slug"`
				ZipURL string `json:"zip_url"`
			} `json:"skills"`
		}
		if jsonErr := json.Unmarshal(data, &index); jsonErr == nil {
			for _, item := range index.Skills {
				if item.Slug == slug && item.ZipURL != "" {
					zipURL = item.ZipURL
					break
				}
			}
		}
	}

	if zipURL == "" {
		primaryURL := fillTpl(primaryFallback, slug)
		fallbackURL := fillTpl(downloadTpl, slug)
		zipURL = primaryURL
		if _, probeErr := s.httpGet(ctx, primaryURL); probeErr != nil {
			zipURL = fallbackURL
		}
	}

	tmpDir, err := os.MkdirTemp("", "skillhub-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, slug+".zip")
	if err := s.downloadFile(ctx, zipURL, zipPath); err != nil {
		return fmt.Errorf("download skillhub zip: %w", err)
	}

	stageDir := filepath.Join(tmpDir, "stage")
	if err := extractZip(zipPath, stageDir); err != nil {
		return fmt.Errorf("extract zip: %w", err)
	}

	targetDir := filepath.Join(st.root, slug)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		backupDir := filepath.Join(st.root,
			fmt.Sprintf(".backup-%s-%d", sanitizePathPart(slug), time.Now().UnixNano()))
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("backup existing: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(backupDir)
		}()
	}

	if err := os.Rename(stageDir, targetDir); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}

	st.mu.Lock()
	st.installed[skill.SkillName] = true
	st.mu.Unlock()
	return nil
}

func (s *Service) installFromChatclaw(ctx context.Context, skill Skill, st *targetState) error {
	downloadURL, err := s.GetSkillDownloadURL(ctx, skill.ID)
	if err != nil {
		return fmt.Errorf("get download URL: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "skillmarket-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, fmt.Sprintf("skill-%d.zip", skill.ID))
	if err := s.downloadFile(ctx, downloadURL, zipPath); err != nil {
		return fmt.Errorf("download skill zip: %w", err)
	}

	stageDir := filepath.Join(tmpDir, "stage")
	if err := extractZip(zipPath, stageDir); err != nil {
		return fmt.Errorf("extract zip: %w", err)
	}

	targetDir := filepath.Join(st.root, skill.SkillName)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		backupDir := filepath.Join(st.root,
			fmt.Sprintf(".backup-%s-%d", sanitizePathPart(skill.SkillName), time.Now().UnixNano()))
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("backup existing: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(backupDir)
		}()
	}

	if err := os.Rename(stageDir, targetDir); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}

	st.mu.Lock()
	st.installed[skill.SkillName] = true
	st.mu.Unlock()
	return nil
}

type OpenClawSkillInfo struct {
	Slug string `json:"slug"`
	Path string `json:"path"`
}

func (s *Service) ListOpenClawSkills(ctx context.Context) ([]OpenClawSkillInfo, error) {
	roots := []string{s.resolveOpenClawSharedRoot()}

	var results []OpenClawSkillInfo
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && isValidSkillDir(filepath.Join(root, entry.Name())) {
				results = append(results, OpenClawSkillInfo{
					Slug: entry.Name(),
					Path: filepath.Join(root, entry.Name()),
				})
			}
		}
	}
	return results, nil
}

func (s *Service) httpGet(ctx context.Context, rawURL string) ([]byte, error) {
	return s.httpGetWithTimeout(ctx, rawURL, 0)
}

func (s *Service) httpGetWithTimeout(ctx context.Context, rawURL string, extraTimeout time.Duration) ([]byte, error) {
	const maxRetries = 5
	backoff := 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(backoff/4) + 1))
			time.Sleep(backoff + jitter)
			backoff = min(backoff*2, 60*time.Second)
		}

		reqCtx := ctx
		if extraTimeout > 0 {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(ctx, extraTimeout)
			defer cancel()
		}

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
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

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("request failed after %d retries", maxRetries)
}

func (s *Service) downloadFile(ctx context.Context, srcURL, destPath string) error {
	return s.downloadFileWithTimeout(ctx, srcURL, destPath, 0)
}

func (s *Service) downloadFileWithTimeout(ctx context.Context, srcURL, destPath string, extraTimeout time.Duration) error {
	reqCtx := ctx
	if extraTimeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, extraTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, srcURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ChatClaw/1.0")
	req.Header.Set("Accept", "application/zip,application/octet-stream,*/*")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func (s *Service) validateSkillDir(name, installRoot string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("skill name is empty")
	}
	if strings.ContainsAny(name, "/\\..") {
		return "", fmt.Errorf("invalid skill name")
	}

	absBase, err := filepath.Abs(installRoot)
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(absBase, name)
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) && absTarget != absBase {
		return "", fmt.Errorf("path traversal not allowed")
	}
	return targetDir, nil
}

func (s *Service) refreshInstalledCacheForScope(scope InstallTargetScope, st *targetState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s.scanSkillDirsUnlocked(st)

	// For openclaw-shared, also scan skills.load.extraDirs from openclaw.json
	if scope == ScopeOpenClawShared {
		s.appendExtraDirsInstalled(st)
	}
}

func (s *Service) scanSkillDirsUnlocked(st *targetState) {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		return
	}
	nowInstalled := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() && isValidSkillDir(filepath.Join(st.root, entry.Name())) {
			nowInstalled[entry.Name()] = true
		}
	}
	st.installed = nowInstalled
}

// isInsideAgentWorkspace checks if the given path is inside any agent workspace directory.
// It checks both the workspace root (e.g. workspace-main/) and the workspace/skills subdirectory
// so that extraDirs pointing directly to workspace-main/skills are also excluded.
func isInsideAgentWorkspace(abs string, wsRoots map[string]bool) bool {
	if wsRoots == nil || abs == "" {
		return false
	}
	abs = filepath.Clean(abs)
	if wsRoots[abs] {
		return true
	}
	// Also check the /skills subdirectory of each workspace root
	for wsRoot := range wsRoots {
		skillsDir := filepath.Join(wsRoot, "skills")
		if abs == skillsDir || strings.HasPrefix(abs, skillsDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Service) appendExtraDirsInstalled(st *targetState) {
	ocRoot, err := define.OpenClawDataRootDir()
	if err != nil {
		return
	}
	// Exclude agent workspace directories from extraDirs scan
	wsRoots := s.agentWorkspaceRoots()

	for _, dir := range readOpenClawExtraDirs(filepath.Join(ocRoot, "openclaw.json")) {
		abs := expandExtraDirPath(dir)
		if abs == "" || abs == st.root {
			continue
		}
		// Skip if this directory is inside an agent workspace (root or /skills subdirectory)
		if isInsideAgentWorkspace(abs, wsRoots) {
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && isValidSkillDir(filepath.Join(abs, entry.Name())) {
				st.installed[entry.Name()] = true
			}
		}
	}
}

// openclawConfigSnip mirrors the shape in openclaw/skills for skills.load.extraDirs
type openclawConfigSnip struct {
	Skills *struct {
		Load *struct {
			ExtraDirs []string `json:"extraDirs"`
		} `json:"load"`
	} `json:"skills"`
}

func readOpenClawExtraDirs(configPath string) []string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var snip openclawConfigSnip
	if err := json.Unmarshal(data, &snip); err != nil {
		return nil
	}
	if snip.Skills == nil || snip.Skills.Load == nil {
		return nil
	}
	return snip.Skills.Load.ExtraDirs
}

// expandExtraDirPath handles ~, {runtimeRoot}, and absolute paths for extraDirs
func expandExtraDirPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// {runtimeRoot} → exe parent directory (app install dir)
	if strings.HasPrefix(p, "{runtimeRoot}") {
		execPath, err := os.Executable()
		if err != nil || strings.TrimSpace(execPath) == "" {
			return ""
		}
		rtRoot := filepath.Dir(execPath)
		rest := strings.TrimPrefix(p, "{runtimeRoot}")
		if rest == "" || rest == "/" {
			return rtRoot
		}
		rest = strings.TrimPrefix(rest, "/")
		return filepath.Join(rtRoot, rest)
	}
	// ~
	home, herr := os.UserHomeDir()
	if p == "~" {
		if herr != nil {
			return ""
		}
		return home
	}
	if strings.HasPrefix(p, "~/") {
		if herr != nil {
			return ""
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(p, "~/")))
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if herr != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(home, p))
}

func resolveRuntimeRoot() (string, error) {
	execPath, err := os.Executable()
	if err != nil || strings.TrimSpace(execPath) == "" {
		return "", fmt.Errorf("cannot resolve executable path")
	}
	return filepath.Dir(execPath), nil
}

func isValidSkillDir(dir string) bool {
	for _, marker := range []string{"SKILL.md", "config.json"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

func sanitizePathPart(input string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	sanitized := replacer.Replace(strings.TrimSpace(input))
	if sanitized == "" {
		return "skill"
	}
	return sanitized
}

func resolvePathUnderBase(baseDir, relativePath string) (string, error) {
	if strings.TrimSpace(relativePath) == "" {
		return "", fmt.Errorf("path is empty")
	}
	cleanRelative := filepath.Clean(filepath.FromSlash(relativePath))
	if cleanRelative == "." {
		return "", fmt.Errorf("path must point to a file or directory")
	}
	if filepath.IsAbs(cleanRelative) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	if cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal not allowed")
	}

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	absPath, err := filepath.Abs(filepath.Join(absBase, cleanRelative))
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	if absPath != absBase && !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return absPath, nil
}

func fillTpl(tpl, slug string) string {
	if tpl == "" {
		return ""
	}
	return strings.ReplaceAll(tpl, "{slug}", url.QueryEscape(slug))
}

func extractZip(zipPath, targetDir string) error {
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zipReader.Close()

	if err := os.RemoveAll(targetDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	for _, f := range zipReader.File {
		name := filepath.FromSlash(f.Name)
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}

		dstPath := filepath.Join(targetDir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}

		src, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// GetTotalSkillCount returns the total number of skills from the remote skill market (not affected by filters/pagination).
func (s *Service) GetTotalSkillCount(ctx context.Context, locale string) (int64, error) {
	baseURL := strings.TrimSuffix(define.ServerURL, "/")
	reqURL := fmt.Sprintf("%s/skill/list?locale=%s&pageSize=1", baseURL, url.QueryEscape(locale))
	body, err := s.httpGet(ctx, reqURL)
	if err != nil {
		return 0, err
	}

	var resp struct {
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse total count response: %w", err)
	}
	return resp.Data.Total, nil
}
