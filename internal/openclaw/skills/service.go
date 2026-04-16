package openclawskills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"chatclaw/internal/define"
	openclawagents "chatclaw/internal/openclaw/agents"
	openclawruntime "chatclaw/internal/openclaw/runtime"

	"gopkg.in/yaml.v3"
)

// OpenClawSkill is a skill from the OpenClaw Gateway (skills.status) and/or on-disk discovery
// following https://docs.openclaw.ai/tools/skills (managed ~/.openclaw/skills, workspace /skills, bundled, extraDirs).
type OpenClawSkill struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	// Icon is the emoji/icon for gateway-sourced skills (e.g. "📊" from SKILL.md frontmatter).
	Icon string `json:"icon"`
	// Permission summarizes declared access (SKILL.md or gateway metadata).
	Permission string `json:"permission"`
	// Scope is optional frontmatter scope when present on disk.
	Scope string `json:"scope"`
	// Location groups UI filters: "shared" (managed, bundled, extra, gateway-global) vs "workspace".
	Location string `json:"location"`
	// DataSource is where the row came from: gateway, managed, workspace, bundled, extra.
	DataSource string `json:"dataSource"`
	// Eligible is set when the list came from skills.status (nil if unknown / disk-only).
	Eligible *bool `json:"eligible,omitempty"`
	// IneligibleReason from gateway when eligible is false.
	IneligibleReason string `json:"ineligibleReason,omitempty"`
	// AgentID is the OpenClaw agent id for workspace-scoped rows.
	AgentID string `json:"agentId"`
	// AgentName is the ChatClaw display name when known.
	AgentName string `json:"agentName"`
	// SkillRoot is the primary (preferred) absolute path to the skill directory.
	// Prefer ScopeRoots for precise lookup by scope; SkillRoot is kept for backward compat.
	SkillRoot string `json:"skillRoot"`
	// ScopeRoots maps scope string -> skill root directory path.
	// Key examples: "openclaw-shared", "local", "agent-workspace:main".
	// This lets callers (especially UI) know exactly where a skill is installed per scope.
	ScopeRoots map[string]string `json:"scopeRoots"`
	// Installations lists every on-disk copy (workspace-*/skills, managed, bundled, extraDirs) for this slug.
	Installations []SkillInstallation `json:"installations,omitempty"`
}

// SkillInstallation is one resolved folder for a skill (multi-workspace / multi-layer).
type SkillInstallation struct {
	OpenClawAgentID string `json:"openclawAgentId"`
	AgentName       string `json:"agentName"`
	SkillRoot       string `json:"skillRoot"`
	// Layer matches DataSource on disk: managed, workspace, bundled, extra.
	Layer string `json:"layer"`
	// Location is shared vs workspace (same semantics as OpenClawSkill.Location).
	Location string `json:"location"`
}

// SkillFileInfo mirrors the native skills binding shape for file previews.
type SkillFileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// OpenClawSkillsService lists skills via OpenClaw Gateway skills.status when connected,
// otherwise reads the same directories OpenClaw uses under OPENCLAW_STATE_DIR (ChatClaw: ~/.chatclaw/openclaw).
type OpenClawSkillsService struct {
	agents *openclawagents.OpenClawAgentsService
	mgr    *openclawruntime.Manager
}

func NewOpenClawSkillsService(agents *openclawagents.OpenClawAgentsService, mgr *openclawruntime.Manager) *OpenClawSkillsService {
	return &OpenClawSkillsService{agents: agents, mgr: mgr}
}

// GetSkillsRoot returns the main agent workspace skills directory (…/workspace-main/skills).
// This matches where OpenClaw CLI `skills install` places skills for the default workspace.
func (s *OpenClawSkillsService) GetSkillsRoot() (string, error) {
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		return "", err
	}
	if err := define.EnsureDataLayout(); err != nil {
		return "", err
	}
	dir := filepath.Join(root, "workspace-"+define.OpenClawMainAgentID, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// GetManagedSkillsRoot returns the optional managed override directory (…/openclaw/skills),
// equivalent to standalone ~/.openclaw/skills under OPENCLAW_STATE_DIR.
func (s *OpenClawSkillsService) GetManagedSkillsRoot() (string, error) {
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		return "", err
	}
	if err := define.EnsureDataLayout(); err != nil {
		return "", err
	}
	dir := filepath.Join(root, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ListSkills prefers Gateway skills.status; falls back to disk layout documented by OpenClaw.
func (s *OpenClawSkillsService) ListSkills() ([]OpenClawSkill, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()

	disk := s.listFromDisk()
	if s.mgr != nil && s.mgr.IsReady() {
		if api := s.collectFromGateway(ctx); api != nil {
			api = dedupeGatewayByCanonicalSlug(api)
			s.applyAgentNames(api)
			return mergeGatewayAndDisk(api, disk), nil
		}
	}
	return mergeGatewayAndDisk(nil, disk), nil
}

// ListSkillsDebug returns all skills found on disk along with the directories that were scanned.
// Useful for diagnosing why a skill does not appear in the list.
func (s *OpenClawSkillsService) ListSkillsDebug() (skills []OpenClawSkill, scannedDirs []string, err error) {
	root, rootErr := define.OpenClawDataRootDir()
	if rootErr != nil {
		return nil, nil, rootErr
	}
	_ = define.EnsureDataLayout()

	agentNames := map[string]string{}
	if s.agents != nil {
		if list, listErr := s.agents.ListAgents(); listErr == nil {
			for _, a := range list {
				agentNames[strings.ToLower(strings.TrimSpace(a.OpenClawAgentID))] = strings.TrimSpace(a.Name)
			}
		}
	}

	// Collect all scanned directories and skills
	scanDir := func(baseDir string) {
		scannedDirs = append(scannedDirs, baseDir)
		entries, readErr := os.ReadDir(baseDir)
		if readErr != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			skillRoot := filepath.Join(baseDir, e.Name())
			skillMd := filepath.Join(skillRoot, "SKILL.md")
			if _, statErr := os.Stat(skillMd); statErr != nil {
				continue
			}
			raw, readErr := os.ReadFile(skillMd)
			if readErr != nil {
				continue
			}
			meta := parseOpenClawSkillFrontmatter(string(raw))
			sk := OpenClawSkill{
				Slug:        e.Name(),
				Name:        meta.Name,
				Description: meta.Description,
				Version:     meta.Version,
				Icon:        meta.Icon,
				Permission:  meta.PermissionSummary(),
				Scope:       meta.Scope,
				SkillRoot:   skillRoot,
			}
			if sk.Name == "" {
				sk.Name = sk.Slug
			}
			skills = append(skills, sk)
		}
	}

	scanDir(filepath.Join(root, "skills"))

	if bundled, err := openclawruntime.BundledSkillsDir(); err == nil {
		scanDir(bundled)
	}

	extraDirs := readSkillExtraDirs(filepath.Join(root, "openclaw.json"))
	if len(extraDirs) > 0 {
		for _, dir := range extraDirs {
			abs, err := expandPath(dir)
			if err != nil || abs == "" {
				continue
			}
			scanDir(abs)
		}
	} else {
		if rtRoot, err := resolveRuntimeRoot(); err == nil {
			scanDir(filepath.Join(rtRoot, "extraSkills"))
		}
	}

	matches, _ := filepath.Glob(filepath.Join(root, "workspace-*"))
	for _, ws := range matches {
		scanDir(filepath.Join(ws, "skills"))
	}

	return skills, scannedDirs, nil
}

func (s *OpenClawSkillsService) collectFromGateway(ctx context.Context) []OpenClawSkill {
	raw, err := s.mgr.SkillsStatus(ctx, "")
	if err != nil {
		return nil
	}
	list, ok := parseSkillsStatusJSON(raw)
	if !ok {
		return nil
	}
	if len(list) > 0 {
		s.applyAgentNames(list)
		return list
	}
	// Global scope returned an empty list — try per-agent (workspace) views.
	var merged []OpenClawSkill
	seen := map[string]struct{}{}
	if s.agents == nil {
		return list
	}
	agents, err := s.agents.ListAgents()
	if err != nil {
		return list
	}
	for _, a := range agents {
		aid := strings.TrimSpace(a.OpenClawAgentID)
		if aid == "" {
			continue
		}
		r2, err2 := s.mgr.SkillsStatus(ctx, aid)
		if err2 != nil {
			continue
		}
		part, ok2 := parseSkillsStatusJSON(r2)
		if !ok2 {
			continue
		}
		for _, sk := range part {
			if strings.TrimSpace(sk.AgentID) == "" {
				sk.AgentID = aid
			}
			if strings.TrimSpace(sk.AgentName) == "" {
				sk.AgentName = strings.TrimSpace(a.Name)
			}
			if sk.Location == "" {
				sk.Location = "workspace"
			}
			k := skillDedupKey(sk)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, sk)
		}
	}
	if len(merged) > 0 {
		return merged
	}
	return list
}

func (s *OpenClawSkillsService) applyAgentNames(list []OpenClawSkill) {
	if s.agents == nil {
		return
	}
	agents, err := s.agents.ListAgents()
	if err != nil {
		return
	}
	byID := map[string]string{}
	for _, a := range agents {
		byID[strings.ToLower(strings.TrimSpace(a.OpenClawAgentID))] = strings.TrimSpace(a.Name)
	}
	for i := range list {
		if list[i].AgentName != "" {
			continue
		}
		aid := strings.TrimSpace(list[i].AgentID)
		if aid == "" {
			continue
		}
		if n, ok := byID[strings.ToLower(aid)]; ok {
			list[i].AgentName = n
		}
	}
}

func skillDedupKey(sk OpenClawSkill) string {
	return strings.ToLower(strings.TrimSpace(sk.AgentID)) + "\x00" + strings.ToLower(strings.TrimSpace(sk.Slug))
}

// canonicalSkillSlug normalizes gateway/disk identifiers so "Quotes", "quotes", and folder names align.
func canonicalSkillSlug(sk OpenClawSkill) string {
	s := strings.TrimSpace(sk.Slug)
	if s == "" {
		s = strings.TrimSpace(sk.Name)
	}
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return strings.Trim(s, "-")
}

func dedupeGatewayByCanonicalSlug(api []OpenClawSkill) []OpenClawSkill {
	groups := map[string][]OpenClawSkill{}
	order := []string{}
	for _, a := range api {
		k := canonicalSkillSlug(a)
		if k == "" {
			continue
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], a)
	}
	out := make([]OpenClawSkill, 0, len(order))
	for _, k := range order {
		out = append(out, bestGatewayRow(groups[k]))
	}
	return out
}

func gatewayRowScore(r OpenClawSkill) int {
	sc := 0
	if r.Eligible != nil && *r.Eligible {
		sc += 100
	}
	sc += len(strings.TrimSpace(r.Description)) / 4
	if strings.TrimSpace(r.Permission) != "" {
		sc += 10
	}
	if strings.TrimSpace(r.Scope) != "" {
		sc += 3
	}
	if strings.TrimSpace(r.AgentID) != "" {
		sc += 1
	}
	return sc
}

func bestGatewayRow(rows []OpenClawSkill) OpenClawSkill {
	if len(rows) == 0 {
		return OpenClawSkill{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		rs, bs := gatewayRowScore(r), gatewayRowScore(best)
		if rs > bs || (rs == bs && len(strings.TrimSpace(r.Description)) > len(strings.TrimSpace(best.Description))) {
			best = r
		}
	}
	return best
}

func pickPrimarySkillRoot(disk []OpenClawSkill) string {
	main := strings.TrimSpace(define.OpenClawMainAgentID)
	for _, d := range disk {
		if strings.EqualFold(strings.TrimSpace(d.AgentID), main) && strings.TrimSpace(d.SkillRoot) != "" {
			return d.SkillRoot
		}
	}
	for _, d := range disk {
		if strings.TrimSpace(d.SkillRoot) != "" {
			return d.SkillRoot
		}
	}
	return ""
}

// scopeKeyForDiskRow returns the scope key used in ScopeRoots map.
// Shared layer (managed/bundled/extra) → "openclaw-shared"
// Workspace layer → "agent-workspace:<agentID>"
func scopeKeyForDiskRow(d OpenClawSkill) string {
	if d.Location == "workspace" && strings.TrimSpace(d.AgentID) != "" {
		return "agent-workspace:" + d.AgentID
	}
	// managed, bundled, extra, gateway (no agent) → shared
	return "openclaw-shared"
}

func mergeGatewayAndDisk(api []OpenClawSkill, disk []OpenClawSkill) []OpenClawSkill {
	bySlugDisk := map[string][]OpenClawSkill{}
	for _, d := range disk {
		k := canonicalSkillSlug(d)
		if k == "" {
			continue
		}
		bySlugDisk[k] = append(bySlugDisk[k], d)
	}
	bySlugAPI := map[string]OpenClawSkill{}
	for _, a := range api {
		k := canonicalSkillSlug(a)
		if k == "" {
			continue
		}
		bySlugAPI[k] = a
	}
	all := map[string]struct{}{}
	for k := range bySlugDisk {
		all[k] = struct{}{}
	}
	for k := range bySlugAPI {
		all[k] = struct{}{}
	}
	slugs := make([]string, 0, len(all))
	for k := range all {
		slugs = append(slugs, k)
	}
	sort.Strings(slugs)
	out := make([]OpenClawSkill, 0, len(slugs))
	for _, slug := range slugs {
		dlist := bySlugDisk[slug]
		gw, hasGW := bySlugAPI[slug]
		row := buildMergedSkillRow(slug, gw, hasGW, dlist)
		if canonicalSkillSlug(row) != "" {
			out = append(out, row)
		}
	}
	return out
}

func buildMergedSkillRow(slug string, gw OpenClawSkill, hasGW bool, dlist []OpenClawSkill) OpenClawSkill {
	var row OpenClawSkill
	switch {
	case hasGW:
		row = gw
		// Ensure ScopeRoots is non-nil so we can assign to it below
		if row.ScopeRoots == nil {
			row.ScopeRoots = make(map[string]string)
		}
	case len(dlist) > 0:
		row = dlist[0]
		if row.ScopeRoots == nil {
			row.ScopeRoots = make(map[string]string)
		}
	default:
		return OpenClawSkill{}
	}
	if len(dlist) > 0 {
		row.Slug = dlist[0].Slug
	} else if strings.TrimSpace(row.Slug) == "" {
		row.Slug = slug
	}
	if strings.TrimSpace(row.Name) == "" {
		row.Name = row.Slug
	}
	// Build ScopeRoots map and Installations from disk results.
	// shared layer (managed/bundled/extra) → key "openclaw-shared"
	// workspace layer → key "agent-workspace:<agentID>"
	scopeRoots := make(map[string]string)
	var inst []SkillInstallation
	for _, d := range dlist {
		if strings.TrimSpace(d.SkillRoot) == "" {
			continue
		}
		scope := scopeKeyForDiskRow(d)
		if scope != "" {
			scopeRoots[scope] = d.SkillRoot
		}
		inst = append(inst, SkillInstallation{
			OpenClawAgentID: d.AgentID,
			AgentName:       d.AgentName,
			SkillRoot:       d.SkillRoot,
			Layer:           d.DataSource,
			Location:        d.Location,
		})
	}
	sort.Slice(inst, func(i, j int) bool {
		ai, aj := inst[i].OpenClawAgentID, inst[j].OpenClawAgentID
		if ai != aj {
			return ai < aj
		}
		return inst[i].SkillRoot < inst[j].SkillRoot
	})
	row.Installations = inst
	row.SkillRoot = pickPrimarySkillRoot(dlist)
	row.ScopeRoots = scopeRoots
	if hasGW {
		for _, d := range dlist {
			if strings.TrimSpace(row.Description) == "" && strings.TrimSpace(d.Description) != "" {
				row.Description = d.Description
			}
			if strings.TrimSpace(row.Permission) == "" && strings.TrimSpace(d.Permission) != "" {
				row.Permission = d.Permission
			}
			if strings.TrimSpace(row.Scope) == "" && strings.TrimSpace(d.Scope) != "" {
				row.Scope = d.Scope
			}
			if strings.TrimSpace(row.Version) == "" && strings.TrimSpace(d.Version) != "" {
				row.Version = d.Version
			}
		}
	}
	return row
}

func parseSkillsStatusJSON(raw json.RawMessage) ([]OpenClawSkill, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err == nil {
		for _, key := range []string{"skills", "items", "entries", "list"} {
			if v, ok := wrap[key]; ok {
				out := mapsToSkillsFromJSONArray(v)
				return out, true
			}
		}
	}
	var rootArr []map[string]any
	if err := json.Unmarshal(raw, &rootArr); err == nil {
		return mapsToSkillsFromMaps(rootArr), true
	}
	return nil, false
}

func mapsToSkillsFromJSONArray(raw json.RawMessage) []OpenClawSkill {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return mapsToSkillsFromMaps(items)
}

func mapsToSkillsFromMaps(items []map[string]any) []OpenClawSkill {
	var out []OpenClawSkill
	for _, m := range items {
		sk := gatewayMapToSkill(m)
		if sk.Slug == "" && sk.Name == "" {
			continue
		}
		if sk.Slug == "" {
			sk.Slug = sk.Name
		}
		if sk.Name == "" {
			sk.Name = sk.Slug
		}
		out = append(out, sk)
	}
	return out
}

func gatewayMapToSkill(m map[string]any) OpenClawSkill {
	sk := OpenClawSkill{
		DataSource:  "gateway",
		Slug:        firstString(m, "slug", "skillKey", "key", "id"),
		Name:        firstString(m, "name", "title", "label"),
		Description: firstString(m, "description", "desc", "summary"),
		Version:     firstString(m, "version"),
		Icon:        firstString(m, "icon", "emoji"),
		AgentID:     firstString(m, "agentId", "agent_id"),
	}
	loc := strings.ToLower(firstString(m, "location", "layer", "source"))
	switch loc {
	case "workspace", "ws", "agent":
		sk.Location = "workspace"
	case "managed", "user", "global", "bundled", "shared", "extra", "plugin":
		sk.Location = "shared"
	default:
		if sk.AgentID != "" {
			sk.Location = "workspace"
		} else {
			sk.Location = "shared"
		}
	}
	// enabled is explicitly toggled by the user in config or UI (true = on, false = off, default = true).
	if v, ok := m["enabled"].(bool); ok {
		sk.Eligible = &v
	} else if v, ok := m["eligible"].(bool); ok {
		// Fallback to eligible for backward compatibility.
		sk.Eligible = &v
	}
	sk.IneligibleReason = firstString(m, "reason", "ineligibleReason", "blockedReason", "ineligible", "gateReason")
	sk.Permission = metadataToPermissionString(m["metadata"])
	if sk.Permission == "" {
		sk.Permission = firstString(m, "permission", "permissions")
	}
	sk.Scope = firstString(m, "scope")
	return sk
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

func metadataToPermissionString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		return fmt.Sprint(t)
	}
}

func (s *OpenClawSkillsService) listFromDisk() []OpenClawSkill {
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		fmt.Printf("[openclaw/skills] listFromDisk: OpenClawDataRootDir error: %v\n", err)
		return nil
	}
	_ = define.EnsureDataLayout()
	fmt.Printf("[openclaw/skills] listFromDisk: root=%s\n", root)

	agentNames := map[string]string{}
	if s.agents != nil {
		if list, listErr := s.agents.ListAgents(); listErr == nil {
			for _, a := range list {
				agentNames[strings.ToLower(strings.TrimSpace(a.OpenClawAgentID))] = strings.TrimSpace(a.Name)
			}
			fmt.Printf("[openclaw/skills] listFromDisk: agents=%v\n", agentNames)
		}
	}

	var out []OpenClawSkill
	sharedRoot := filepath.Join(root, "skills")
	fmt.Printf("[openclaw/skills] listFromDisk: scanning shared root=%s\n", sharedRoot)
	out = append(out, scanSkillsUnder(sharedRoot, "shared", "", "", "managed", agentNames)...)

	if bundled, err := openclawruntime.BundledSkillsDir(); err == nil {
		fmt.Printf("[openclaw/skills] listFromDisk: scanning bundled=%s\n", bundled)
		out = append(out, scanSkillsUnder(bundled, "shared", "", "", "bundled", agentNames)...)
	}

	extraDirs := readSkillExtraDirs(filepath.Join(root, "openclaw.json"))
	fmt.Printf("[openclaw/skills] listFromDisk: extraDirs from config=%v\n", extraDirs)
	if len(extraDirs) > 0 {
		for _, dir := range extraDirs {
			abs, err := expandPath(dir)
			if err != nil || abs == "" {
				fmt.Printf("[openclaw/skills] listFromDisk: extraDir=%s skipped (expand error or empty)\n", dir)
				continue
			}
			fmt.Printf("[openclaw/skills] listFromDisk: scanning extraDir=%s (resolved=%s)\n", dir, abs)
			out = append(out, scanSkillsUnder(abs, "shared", "", "", "extra", agentNames)...)
		}
	} else {
		if rtRoot, err := resolveRuntimeRoot(); err == nil {
			defaultExtra := filepath.Join(rtRoot, "extraSkills")
			fmt.Printf("[openclaw/skills] listFromDisk: scanning default extraSkills=%s\n", defaultExtra)
			out = append(out, scanSkillsUnder(defaultExtra, "shared", "", "", "extra", agentNames)...)
		}
	}

	matches, _ := filepath.Glob(filepath.Join(root, "workspace-*"))
	fmt.Printf("[openclaw/skills] listFromDisk: found workspace dirs=%v\n", matches)
	for _, ws := range matches {
		wsSkillsDir := filepath.Join(ws, "skills")
		fmt.Printf("[openclaw/skills] listFromDisk: scanning workspace skills dir=%s\n", wsSkillsDir)
		out = append(out, scanSkillsUnder(wsSkillsDir, "workspace", strings.TrimPrefix(filepath.Base(ws), "workspace-"), agentNames[strings.ToLower(strings.TrimPrefix(filepath.Base(ws), "workspace-"))], "workspace", agentNames)...)
	}

	fmt.Printf("[openclaw/skills] listFromDisk: total=%d skills\n", len(out))
	for _, sk := range out {
		fmt.Printf("  skill: slug=%s location=%s dataSource=%s agentId=%s skillRoot=%s\n", sk.Slug, sk.Location, sk.DataSource, sk.AgentID, sk.SkillRoot)
	}
	return out
}

type openclawConfigSnip struct {
	Skills *struct {
		Load *struct {
			ExtraDirs []string `json:"extraDirs"`
		} `json:"load"`
	} `json:"skills"`
}

func readSkillExtraDirs(configPath string) []string {
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

// resolveRuntimeRoot returns the directory containing the running executable (app install dir),
// e.g. C:\soft\ChatClaw. This is the root for bundled rt/, build/, extraSkills/.
func resolveRuntimeRoot() (string, error) {
	execPath, err := os.Executable()
	if err != nil || strings.TrimSpace(execPath) == "" {
		return "", fmt.Errorf("cannot resolve executable path")
	}
	return filepath.Dir(execPath), nil
}

func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}

	// Handle {runtimeRoot} placeholder — resolves to the app's install directory
	// (exe parent dir, e.g. C:\soft\ChatClaw), which contains rt/, build/, extraSkills/
	if strings.HasPrefix(p, "{runtimeRoot}") {
		rtRoot, err := resolveRuntimeRoot()
		if err != nil {
			return "", err
		}
		rest := strings.TrimPrefix(p, "{runtimeRoot}")
		if rest == "" || rest == "/" {
			return rtRoot, nil
		}
		rest = strings.TrimPrefix(rest, "/")
		return filepath.Join(rtRoot, rest), nil
	}

	home, herr := os.UserHomeDir()
	if p == "~" {
		if herr != nil {
			return "", herr
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		if herr != nil {
			return "", herr
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(p, "~/"))), nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if herr != nil {
		return filepath.Clean(p), nil
	}
	return filepath.Clean(filepath.Join(home, p)), nil
}

// ReadSkillMarkdown returns SKILL.md content for the given skill root.
func (s *OpenClawSkillsService) ReadSkillMarkdown(skillRoot string) (string, error) {
	if err := s.mustBeAllowedSkillRoot(skillRoot); err != nil {
		return "", err
	}
	p := filepath.Join(skillRoot, "SKILL.md")
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}
	return string(data), nil
}

// ListSkillFiles lists files under a skill directory (for preview).
func (s *OpenClawSkillsService) ListSkillFiles(skillRoot string) ([]SkillFileInfo, error) {
	if err := s.mustBeAllowedSkillRoot(skillRoot); err != nil {
		return nil, err
	}
	var files []SkillFileInfo
	err := filepath.Walk(skillRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(skillRoot, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, SkillFileInfo{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortSkillFiles(files)
	return files, nil
}

// ReadSkillFile reads a file under the skill root (relative path uses /).
func (s *OpenClawSkillsService) ReadSkillFile(skillRoot, filePath string) (string, error) {
	if err := s.mustBeAllowedSkillRoot(skillRoot); err != nil {
		return "", err
	}
	rel := filepath.FromSlash(strings.TrimPrefix(strings.TrimSpace(filePath), "/"))
	full := filepath.Join(skillRoot, rel)
	absSkill, err := filepath.Abs(skillRoot)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if absFull != absSkill && !strings.HasPrefix(absFull, absSkill+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal not allowed")
	}
	data, err := os.ReadFile(absFull)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// EnableSkill enables a gateway skill (calls skills.enable RPC). agentID can be empty for default scope.
func (s *OpenClawSkillsService) EnableSkill(ctx context.Context, skillSlug, agentID string) error {
	if s.mgr == nil || !s.mgr.IsReady() {
		return errors.New("gateway not connected")
	}
	params := map[string]any{"skillKey": skillSlug}
	if strings.TrimSpace(agentID) != "" {
		params["agentId"] = strings.TrimSpace(agentID)
	}
	return s.mgr.Request(ctx, "skills.enable", params, nil)
}

// DisableSkill disables a gateway skill (calls skills.disable RPC). agentID can be empty for default scope.
func (s *OpenClawSkillsService) DisableSkill(ctx context.Context, skillSlug, agentID string) error {
	if s.mgr == nil || !s.mgr.IsReady() {
		return errors.New("gateway not connected")
	}
	params := map[string]any{"skillKey": skillSlug}
	if strings.TrimSpace(agentID) != "" {
		params["agentId"] = strings.TrimSpace(agentID)
	}
	return s.mgr.Request(ctx, "skills.disable", params, nil)
}

func (s *OpenClawSkillsService) mustBeAllowedSkillRoot(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	roots, err := s.allowedSkillRoots()
	if err != nil {
		return err
	}
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, absPath)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path outside allowed OpenClaw skill roots")
}

func (s *OpenClawSkillsService) allowedSkillRoots() ([]string, error) {
	var roots []string
	if oc, err := define.OpenClawDataRootDir(); err == nil {
		roots = append(roots, oc)
	}
	if b, err := openclawruntime.BundledSkillsDir(); err == nil {
		roots = append(roots, b)
	}
	if oc, err := define.OpenClawDataRootDir(); err == nil {
		configPath := filepath.Join(oc, "openclaw.json")
		extraDirs := readSkillExtraDirs(configPath)
		if len(extraDirs) > 0 {
			for _, dir := range extraDirs {
				abs, err := expandPath(dir)
				if err != nil || abs == "" {
					continue
				}
				roots = append(roots, abs)
			}
		} else {
			// Default extraSkills/ at runtime root (exe directory)
			if rtRoot, err := resolveRuntimeRoot(); err == nil {
				roots = append(roots, filepath.Join(rtRoot, "extraSkills"))
			}
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no skill roots configured")
	}
	return roots, nil
}

func scanSkillsUnder(
	baseDir string,
	location string,
	agentID string,
	agentName string,
	dataSource string,
	_ map[string]string,
) []OpenClawSkill {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var out []OpenClawSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		skillRoot := filepath.Join(baseDir, name)
		skillMd := filepath.Join(skillRoot, "SKILL.md")
		if _, statErr := os.Stat(skillMd); statErr != nil {
			continue
		}
		raw, readErr := os.ReadFile(skillMd)
		if readErr != nil {
			continue
		}
		meta := parseOpenClawSkillFrontmatter(string(raw))
		sk := OpenClawSkill{
			Slug:        name,
			Name:        meta.Name,
			Description: meta.Description,
			Version:     meta.Version,
			Icon:        meta.Icon,
			Permission:  meta.PermissionSummary(),
			Scope:       meta.Scope,
			Location:    location,
			DataSource:  dataSource,
			AgentID:     agentID,
			AgentName:   agentName,
			SkillRoot:   skillRoot,
		}
		if sk.Name == "" {
			sk.Name = sk.Slug
		}
		out = append(out, sk)
	}
	return out
}

type openClawSkillMeta struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Version     string      `yaml:"version"`
	Icon        string      `yaml:"icon"`
	Permission  string      `yaml:"permission"`
	Permissions interface{} `yaml:"permissions"`
	Scope       string      `yaml:"scope"`
}

func (m *openClawSkillMeta) PermissionSummary() string {
	if strings.TrimSpace(m.Permission) != "" {
		return strings.TrimSpace(m.Permission)
	}
	if m.Permissions == nil {
		return ""
	}
	switch v := m.Permissions.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, ", ")
	case []string:
		return strings.Join(v, ", ")
	default:
		return fmt.Sprint(v)
	}
}

func parseOpenClawSkillFrontmatter(data string) openClawSkillMeta {
	data = strings.TrimSpace(data)
	const delim = "---"
	if !strings.HasPrefix(data, delim) {
		return openClawSkillMeta{}
	}
	rest := data[len(delim):]
	endIdx := strings.Index(rest, "\n"+delim)
	if endIdx == -1 {
		return openClawSkillMeta{}
	}
	front := strings.TrimSpace(rest[:endIdx])
	var meta openClawSkillMeta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return openClawSkillMeta{}
	}
	return meta
}

func sortSkillFiles(files []SkillFileInfo) {
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if skillFileLess(files[i].Path, files[j].Path) > 0 {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

// GetDisabledSkillSlugs reads openclaw.json and returns a map of disabled skill slugs.
// A skill is considered disabled if skills.entries.<slug>.enabled is explicitly false.
// Skills not present in entries are considered enabled by default.
func (s *OpenClawSkillsService) GetDisabledSkillSlugs() (map[string]bool, error) {
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		return nil, err
	}
	_ = define.EnsureDataLayout()
	configPath := filepath.Join(root, "openclaw.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	skillsRaw, ok := raw["skills"].(map[string]any)
	if !ok {
		return nil, nil
	}
	entries, ok := skillsRaw["entries"].(map[string]any)
	if !ok {
		return nil, nil
	}
	disabled := make(map[string]bool)
	for slug, v := range entries {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			disabled[slug] = true
		}
	}
	return disabled, nil
}

// SetSkillEnabled reads openclaw.json, sets or clears skills.entries.<slug>.enabled,
// and writes the file back. This mirrors `openclaw config set skills.entries.<slug>.enabled <val>`.
func (s *OpenClawSkillsService) SetSkillEnabled(slug string, enabled bool) error {
	if strings.TrimSpace(slug) == "" {
		return errors.New("slug cannot be empty")
	}
	root, err := define.OpenClawDataRootDir()
	if err != nil {
		return err
	}
	_ = define.EnsureDataLayout()
	configPath := filepath.Join(root, "openclaw.json")

	// Read existing content or start fresh.
	var raw map[string]any
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse openclaw.json: %w", err)
		}
	} else {
		raw = make(map[string]any)
	}

	if raw["skills"] == nil {
		raw["skills"] = make(map[string]any)
	}
	skillsRaw, ok := raw["skills"].(map[string]any)
	if !ok {
		raw["skills"] = make(map[string]any)
		skillsRaw = raw["skills"].(map[string]any)
	}
	if skillsRaw["entries"] == nil {
		skillsRaw["entries"] = make(map[string]any)
	}
	entries, ok := skillsRaw["entries"].(map[string]any)
	if !ok {
		skillsRaw["entries"] = make(map[string]any)
		entries = skillsRaw["entries"].(map[string]any)
	}

	slug = strings.TrimSpace(slug)
	if enabled {
		// Remove the entry key when enabling (default is enabled).
		delete(entries, slug)
	} else {
		if entries[slug] == nil {
			entries[slug] = make(map[string]any)
		}
		entry, ok := entries[slug].(map[string]any)
		if !ok {
			entry = make(map[string]any)
			entries[slug] = entry
		}
		entry["enabled"] = false
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal openclaw.json: %w", err)
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return fmt.Errorf("write openclaw.json: %w", err)
	}
	return nil
}

func skillFileLess(a, b string) int {
	if a == "SKILL.md" {
		return -1
	}
	if b == "SKILL.md" {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
