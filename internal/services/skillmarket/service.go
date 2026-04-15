package skillmarket

import (
	"archive/zip"
	"context"
	"encoding/json"
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

	agentSvc "chatclaw/internal/services/agents"
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
	Scope             InstallTargetScope `json:"scope"`
	Path              string            `json:"path"`
	Label             string            `json:"label"`
	Available         bool              `json:"available"`
	OpenClawAgentID  string            `json:"openClawAgentId,omitempty"`
}

type Service struct {
	app        *application.App
	httpClient *http.Client
	agents     *agentSvc.AgentsService
}

type targetState struct {
	root      string
	mu        sync.RWMutex
	installed map[string]bool
}

func NewService(app *application.App, agents *agentSvc.AgentsService) *Service {
	return &Service{
		app:        app,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		agents:     agents,
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

// resolveAgentWorkspaceRoot returns the skills directory for a specific OpenClaw agent.
// Format: <openclaw_root>/workspace-<openClawAgentID>/skills
func (s *Service) resolveAgentWorkspaceRoot(openClawAgentID string) string {
	root, _ := define.OpenClawDataRootDir()
	return filepath.Join(root, fmt.Sprintf("workspace-%s", openClawAgentID), "skills")
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
		root = s.resolveAgentWorkspaceRoot(openClawAgentID)
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
	agent, err := s.agents.GetAgent(agentID)
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
			Scope:             AgentWorkspaceScope(openClawAgentID),
			Path:              st.root,
			Label:             fmt.Sprintf("%s 工作目录", agent.Name),
			Available:         true,
			OpenClawAgentID:  openClawAgentID,
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
	CategoryID *int64            `json:"categoryId,omitempty"`
	Name       string            `json:"name,omitempty"`
	Locale     string            `json:"locale,omitempty"`
	Page       int               `json:"page,omitempty"`
	PageSize   int               `json:"pageSize,omitempty"`
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
			Total int64  `json:"total"`
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

	switch skill.Source {
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

	targetDir, err := s.validateSkillDir(skillName, st.root)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("remove skill directory: %w", err)
	}

	st.mu.Lock()
	delete(st.installed, skillName)
	st.mu.Unlock()
	return nil
}

func (s *Service) installFromClawhub(ctx context.Context, skill Skill, st *targetState) error {
	const baseURL = "https://clawhub.ai/api/v1"
	slug := skill.SkillName

	metaURL := fmt.Sprintf("%s/skills/%s", baseURL, url.PathEscape(slug))
	body, err := s.httpGet(ctx, metaURL)
	if err != nil {
		return fmt.Errorf("fetch clawhub skill metadata: %w", err)
	}

	var metaResp struct {
		LatestVersion *struct {
			Version string `json:"version"`
			Files   []struct {
				Path string `json:"path"`
				Size int64  `json:"size"`
			} `json:"files"`
		} `json:"latestVersion"`
	}
	if err := json.Unmarshal(body, &metaResp); err != nil {
		return fmt.Errorf("parse clawhub meta: %w", err)
	}

	if metaResp.LatestVersion == nil || metaResp.LatestVersion.Version == "" {
		return fmt.Errorf("cannot resolve version for %s", slug)
	}
	version := metaResp.LatestVersion.Version
	files := metaResp.LatestVersion.Files

	skillsBaseDir, err := filepath.Abs(st.root)
	if err != nil {
		return fmt.Errorf("resolve skills directory: %w", err)
	}

	stagingPrefix := ".install-" + sanitizePathPart(slug) + "-"
	stagingDir, err := os.MkdirTemp(skillsBaseDir, stagingPrefix)
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	for _, f := range files {
		fileURL := fmt.Sprintf("%s/skills/%s/file?path=%s&version=%s",
			baseURL, url.PathEscape(slug), url.QueryEscape(f.Path), url.QueryEscape(version))
		content, dlErr := s.httpGet(ctx, fileURL)
		if dlErr != nil {
			return fmt.Errorf("download %s: %w", f.Path, dlErr)
		}

		filePath, pathErr := resolvePathUnderBase(stagingDir, f.Path)
		if pathErr != nil {
			return fmt.Errorf("invalid path %q: %w", f.Path, pathErr)
		}
		if dir := filepath.Dir(filePath); dir != stagingDir {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", f.Path, err)
			}
		}
		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.Path, err)
		}
	}

	targetDir := filepath.Join(st.root, slug)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		backupDir := filepath.Join(skillsBaseDir,
			fmt.Sprintf(".backup-%s-%d", sanitizePathPart(slug), time.Now().UnixNano()))
		if err := os.Rename(targetDir, backupDir); err != nil {
			return fmt.Errorf("backup existing: %w", err)
		}
		defer func() {
			_ = os.RemoveAll(backupDir)
		}()
	}

	if err := os.Rename(stagingDir, targetDir); err != nil {
		return fmt.Errorf("activate skill: %w", err)
	}
	cleanupStaging = false

	st.mu.Lock()
	st.installed[skill.SkillName] = true
	st.mu.Unlock()
	return nil
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
	const maxRetries = 5
	backoff := 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(backoff/4) + 1))
			time.Sleep(backoff + jitter)
			backoff = min(backoff*2, 60*time.Second)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ChatClaw/1.0")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
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

func (s *Service) appendExtraDirsInstalled(st *targetState) {
	ocRoot, err := define.OpenClawDataRootDir()
	if err != nil {
		return
	}
	for _, dir := range readOpenClawExtraDirs(filepath.Join(ocRoot, "openclaw.json")) {
		abs := expandExtraDirPath(dir)
		if abs == "" || abs == st.root {
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

	for _, f := range zipReader.File {
		name := filepath.FromSlash(f.Name)
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}
	}

	var commonPrefix string
	for _, f := range zipReader.File {
		name := filepath.ToSlash(filepath.Clean(f.Name))
		parts := strings.Split(name, "/")
		if len(parts) <= 1 {
			continue
		}
		prefix := strings.Join(parts[:len(parts)-1], "/")
		if commonPrefix == "" || strings.HasPrefix(prefix, commonPrefix+"/") {
			commonPrefix = prefix
		}
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	for _, f := range zipReader.File {
		name := filepath.FromSlash(f.Name)
		if commonPrefix != "" && strings.HasPrefix(name, commonPrefix+"/") {
			name = strings.TrimPrefix(name, commonPrefix+"/")
		}
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}

		dstPath := filepath.Join(targetDir, name)
		os.MkdirAll(filepath.Dir(dstPath), 0o755)

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
