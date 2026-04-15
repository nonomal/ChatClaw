package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	lockfileName            = ".skills_store_lock.json"
	skillConfigName         = "config.json"
	skillMetaName           = "_meta.json"
	defaultVersion           = "2026.3.3"
	defaultIndexFallback    = "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills.json"
	defaultSearchFallback   = "http://lightmake.site/api/v1/search"
	defaultUpgradeFallback  = "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/version.json"
	defaultDownloadFallback = "https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/skills/{slug}.zip"
	defaultPrimaryFallback  = "http://lightmake.site/api/v1/download?slug={slug}"
	defaultInstallRoot      = "~/.chatclaw/openclaw/skills"
)

type lockfile struct {
	Version int        `json:"version"`
	Skills  SkillsLock `json:"skills"`
}

type SkillsLock map[string]SkillLockEntry

type SkillLockEntry struct {
	Name    string `json:"name"`
	ZipURL  string `json:"zip_url"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: skillhub <command> [args]")
		fmt.Fprintln(os.Stderr, "commands: search, install, list, upgrade, version")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println(defaultVersion)
	case "search":
		searchCmd(defaultIndexFallback, defaultSearchFallback)
	case "install":
		installCmd(defaultIndexFallback, defaultSearchFallback, defaultDownloadFallback, defaultPrimaryFallback)
	case "list":
		listCmd()
	case "upgrade":
		upgradeCmd(defaultUpgradeFallback)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

func expandUser(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func strVal(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// ---------------------------------------------------------------------------
// Search command
// ---------------------------------------------------------------------------

func searchCmd(indexURL, searchURL string) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillhub search [query...]")
	}
	fsURL := fs.String("search-url", searchURL, "")
	limit := fs.Int("limit", 20, "")
	timeout := fs.Int("timeout", 6, "")
	_ = fs.Parse(os.Args[2:])

	q := strings.TrimSpace(strings.Join(fs.Args(), " "))

	if q != "" {
		results := remoteSearch(*fsURL, q, *limit, *timeout)
		if results != nil && len(results) > 0 {
			fmt.Println("You can use \"skillhub install [skill]\" to install.")
			for _, r := range results {
				fmt.Printf("%s  %s\n", r["slug"], r["name"])
				if d, _ := r["description"].(string); d != "" {
					fmt.Printf("  - %s\n", d)
				}
				if v, _ := r["version"].(string); v != "" {
					fmt.Printf("  - version: %s\n", v)
				}
			}
			return
		}
	}

	data, err := fetchJSON(indexURL, 20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: failed to load index: %v\n", err)
		data = map[string]any{"skills": []any{}}
	}
	skills, _ := data["skills"].([]any)
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		return
	}

	matches := make([]map[string]any, 0, len(skills))
	for _, s := range skills {
		if m, ok := s.(map[string]any); ok {
			matches = append(matches, m)
		}
	}

	if q != "" {
		ql := strings.ToLower(q)
		sort.Slice(matches, func(i, j int) bool {
			return rankSkill(matches[i], ql) > rankSkill(matches[j], ql)
		})
	}

	fmt.Println("You can use \"skillhub install [skill]\" to install.")
	for _, m := range matches {
		slug := str(m["slug"])
		name := str(m["name"])
		if name == "" {
			name = slug
		}
		desc := str(m["description"])
		if desc == "" {
			desc = str(m["summary"])
		}
		zipURL := str(m["zip_url"])
		homepage := str(m["homepage"])
		version := str(m["version"])

		fmt.Printf("%s  %s\n", slug, name)
		if desc != "" {
			fmt.Printf("  - %s\n", desc)
		}
		if version != "" {
			fmt.Printf("  - version: %s\n", version)
		}
		if zipURL != "" {
			fmt.Printf("  - %s\n", zipURL)
		}
		if homepage != "" && !isClawhubURL(homepage) {
			fmt.Printf("  - %s\n", homepage)
		}
	}
}

func rankSkill(skill map[string]any, query string) int {
	var sb strings.Builder
	add := func(v any) {
		if s, ok := v.(string); ok {
			sb.WriteString(s)
			sb.WriteByte(' ')
		}
	}
	add(skill["slug"])
	add(skill["name"])
	add(skill["description"])
	add(skill["summary"])
	add(skill["version"])
	if tags, ok := skill["tags"].([]any); ok {
		for _, t := range tags {
			add(t)
		}
	}
	if cats, ok := skill["categories"].([]any); ok {
		for _, c := range cats {
			add(c)
		}
	}
	return strings.Count(strings.ToLower(sb.String()), query)
}

func isClawhubURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "clawhub.ai" || strings.HasSuffix(host, ".clawhub.ai")
}

func remoteSearch(searchURL, query string, limit, timeout int) []map[string]any {
	if searchURL == "" || query == "" {
		return nil
	}
	u, err := url.Parse(searchURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	var data map[string]any
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		return nil
	}
	results, _ := data["results"].([]any)
	if len(results) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		slug := str(m["slug"])
		if slug == "" {
			continue
		}
		name := strVal(strVal(m["displayName"].(string), ""), m["name"].(string))
		if name == "" {
			name = slug
		}
		out = append(out, map[string]any{
			"slug":        slug,
			"name":        name,
			"description": strVal(m["summary"].(string), m["description"].(string)),
			"version":     str(m["version"]),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Install command
// ---------------------------------------------------------------------------

func installCmd(indexURL, searchURL, downloadTpl, primaryTpl string) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillhub install <slug> [--dir <path>] [--force]")
	}
	fsDir := fs.String("dir", defaultInstallRoot, "")
	fsForce := fs.Bool("force", false, "")
	fsBase := fs.String("files-base-uri", "", "")
	fsTpl := fs.String("download-url-template", downloadTpl, "")
	fsPrimary := fs.String("primary-download-url-template", primaryTpl, "")
	fsURL := fs.String("search-url", searchURL, "")
	fsLimit := fs.Int("search-limit", 20, "")
	fsTimeout := fs.Int("search-timeout", 6, "")
	_ = fs.Parse(os.Args[2:])

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "error: slug is required")
		os.Exit(1)
	}
	slug := fs.Arg(0)

	installRoot := expandUser(*fsDir)
	targetDir := filepath.Join(installRoot, slug)

	if exists, _ := pathExists(targetDir); exists && !*fsForce {
		fmt.Fprintf(os.Stderr, "Error: Target exists: %s (use --force to overwrite)\n", targetDir)
		os.Exit(1)
	}

	// Look up skill from index
	data, _ := fetchJSON(indexURL, 20)
	skills, _ := data["skills"].([]any)
	var skill map[string]any
	var indexHit bool
	for _, s := range skills {
		if m, ok := s.(map[string]any); ok && str(m["slug"]) == slug {
			skill = m
			indexHit = true
			break
		}
	}

	if skill == nil {
		results := remoteSearch(*fsURL, slug, *fsLimit, *fsTimeout)
		if results != nil {
			for _, r := range results {
				if r["slug"] == slug {
					skill = r
					fmt.Fprintf(os.Stderr, "info: %q not in index, using remote registry exact match\n", slug)
					break
				}
			}
		}
	}

	if skill == nil {
		skill = map[string]any{"slug": slug, "name": slug, "version": "", "source": "skillhub"}
		fmt.Fprintf(os.Stderr, "info: %q not in index/remote search, try direct download by slug\n", slug)
	}

	var fallbackURL string
	if indexHit {
		fallbackURL = resolveZipURI(skill, slug, *fsBase, *fsTpl)
	} else {
		fallbackURL = fillTpl(*fsTpl, slug)
	}
	primaryURL := fillTpl(*fsPrimary, slug)
	sha256Val := str(skill["sha256"])

	if err := installWithFallback(slug, []string{primaryURL, fallbackURL}, targetDir, sha256Val); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	lock := loadLock(installRoot)
	name := str(skill["name"])
	if name == "" {
		name = slug
	}
	source := strVal(str(skill["source"]), "skillhub")
	zipURL := primaryURL
	if zipURL == "" {
		zipURL = fallbackURL
	}
	if lock.Skills == nil {
		lock.Skills = make(SkillsLock)
	}
	lock.Skills[slug] = SkillLockEntry{
		Name:    name,
		ZipURL:  zipURL,
		Source:  source,
		Version: str(skill["version"]),
	}
	saveLock(installRoot, lock)
	updateClawhubLock(slug, str(skill["version"]))

	fmt.Printf("Installed: %s -> %s\n", slug, targetDir)
}

func resolveZipURI(skill map[string]any, slug, filesBase, tpl string) string {
	if filesBase != "" {
		return fillTpl(filesBase, slug)
	}
	for _, key := range []string{"zip_url", "zipUrl", "archive_url", "archiveUrl", "file_url", "fileUrl"} {
		if v := str(skill[key]); v != "" {
			u, err := url.Parse(v)
			if err == nil && u.Scheme != "" {
				return v
			}
			return "file://" + filepath.ToSlash(expandUser(v))
		}
	}
	if tpl != "" {
		return fillTpl(tpl, slug)
	}
	return ""
}

func installWithFallback(slug string, uris []string, targetDir, sha256Val string) error {
	var lastErr error
	for _, uri := range uris {
		if uri == "" {
			continue
		}
		if err := installZip(slug, uri, targetDir, sha256Val); err == nil {
			return nil
		} else {
			lastErr = err
			fmt.Fprintf(os.Stderr, "Download failed, trying next source: %v\n", err)
		}
	}
	return fmt.Errorf("all download sources failed: %v", lastErr)
}

func installZip(slug, zipURI, targetDir, expectedSHA string) error {
	if exists, _ := pathExists(targetDir); exists {
		os.RemoveAll(targetDir)
	}

	tmp, err := os.MkdirTemp("", "skillhub-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	zipPath := filepath.Join(tmp, slug+".zip")
	stageDir := filepath.Join(tmp, "stage")

	fmt.Printf("Downloading: %s\n", zipURI)
	if err := downloadFile(zipURI, zipPath); err != nil {
		return err
	}
	if expectedSHA != "" {
		if actual := sha256File(zipPath); !strings.EqualFold(actual, expectedSHA) {
			return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA, actual)
		}
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}
	if err := extractZip(zipPath, stageDir); err != nil {
		return fmt.Errorf("extract zip: %w", err)
	}
	os.MkdirAll(filepath.Dir(targetDir), 0o755)
	if err := os.Rename(stageDir, targetDir); err != nil {
		return fmt.Errorf("move stage to target: %w", err)
	}
	return nil
}

func extractZip(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("not a valid zip: %w", err)
	}
	defer reader.Close()

	for _, f := range reader.File {
		name := filepath.FromSlash(f.Name)
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}
	}

	for _, f := range reader.File {
		name := filepath.FromSlash(f.Name)
		dstPath := filepath.Join(targetDir, name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(dstPath, 0o755)
			continue
		}
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

// ---------------------------------------------------------------------------
// List command
// ---------------------------------------------------------------------------

func listCmd() {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fsDir := fs.String("dir", defaultInstallRoot, "")
	_ = fs.Parse(os.Args[2:])

	lock := loadLock(expandUser(*fsDir))
	if len(lock.Skills) == 0 {
		fmt.Println("No installed skills.")
		return
	}
	slugs := make([]string, 0, len(lock.Skills))
	for s := range lock.Skills {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		entry := lock.Skills[s]
		fmt.Printf("%s  %s\n", s, entry.Version)
	}
}

// ---------------------------------------------------------------------------
// Upgrade command
// ---------------------------------------------------------------------------

func upgradeCmd(upgradeURL string) {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skillhub upgrade [slug] [--dir <path>] [--check-only]")
	}
	fsDir := fs.String("dir", defaultInstallRoot, "")
	fsCheck := fs.Bool("check-only", false, "")
	fsTimeout := fs.Int("timeout", 20, "")
	_ = fs.Parse(os.Args[2:])

	slug := ""
	if fs.NArg() > 0 {
		slug = fs.Arg(0)
	}

	installRoot := expandUser(*fsDir)
	lock := loadLock(installRoot)
	if lock.Skills == nil {
		lock.Skills = make(SkillsLock)
	}

	var targets []string
	if slug != "" {
		targets = []string{slug}
	} else {
		targets = make([]string, 0, len(lock.Skills))
		for s := range lock.Skills {
			targets = append(targets, s)
		}
		sort.Strings(targets)
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "No installed skills in lockfile: %s\n", filepath.Join(installRoot, lockfileName))
		os.Exit(1)
	}

	var result upgradeResult
	for _, s := range targets {
		result.Checked++
		targetDir := filepath.Join(installRoot, s)

		if exists, _ := pathExists(targetDir); !exists {
			fmt.Printf("[%s] skip: skill directory not found: %s\n", s, targetDir)
			result.Skipped++
			continue
		}

		configPath := filepath.Join(targetDir, skillConfigName)
		configData, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("[%s] skip: %s not found\n", s, skillConfigName)
			result.Skipped++
			continue
		}
		var config map[string]any
		if json.Unmarshal(configData, &config) != nil {
			fmt.Printf("[%s] fail: invalid %s\n", s, skillConfigName)
			result.Failed++
			continue
		}

		updateURL := extractUpdateURL(config, targetDir)
		if updateURL == "" {
			fmt.Printf("[%s] skip: missing update URL in %s\n", s, skillConfigName)
			result.Skipped++
			continue
		}

		manifest, err := fetchJSON(updateURL, *fsTimeout)
		if err != nil {
			fmt.Printf("[%s] fail: fetch manifest: %v\n", s, err)
			result.Failed++
			continue
		}

		latestVersion := firstStr(manifest, "version", "latest_version", "latestVersion")
		latestURL := firstStr(manifest, "zip_url", "zipUrl", "download_url", "downloadUrl", "package_url", "packageUrl", "url")
		latestSHA := firstStr(manifest, "sha256", "sha_256", "checksum")

		if latestVersion == "" {
			fmt.Printf("[%s] fail: update manifest missing version: %s\n", s, updateURL)
			result.Failed++
			continue
		}
		if latestURL == "" {
			fmt.Printf("[%s] fail: update manifest missing package URL: %s\n", s, updateURL)
			result.Failed++
			continue
		}

		curVersion := lock.Skills[s].Version
		if curVersion == "" {
			metaPath := filepath.Join(targetDir, skillMetaName)
			if metaData, err := os.ReadFile(metaPath); err == nil {
				var meta map[string]any
				if json.Unmarshal(metaData, &meta) == nil {
					curVersion = firstStr(meta, "version")
				}
			}
		}

		if !versionIsNewer(latestVersion, curVersion) {
			fmt.Printf("[%s] up-to-date: current=%s latest=%s\n", s, cv(curVersion), latestVersion)
			result.Skipped++
			continue
		}

		if *fsCheck {
			fmt.Printf("[%s] upgrade available: current=%s latest=%s package=%s\n", s, cv(curVersion), latestVersion, latestURL)
			continue
		}

		if err := installZip(s, latestURL, targetDir, latestSHA); err != nil {
			fmt.Printf("[%s] fail: %v\n", s, err)
			result.Failed++
			continue
		}
		os.WriteFile(configPath, configData, 0o644)

		lock.Skills[s] = SkillLockEntry{
			Name:    s,
			ZipURL:  latestURL,
			Source:  "unknown",
			Version: latestVersion,
		}
		result.Upgraded++
		fmt.Printf("[%s] upgraded: %s -> %s\n", s, cv(curVersion), latestVersion)
	}

	saveLock(installRoot, lock)
	fmt.Printf("upgrade done: checked=%d upgraded=%d skipped=%d failed=%d dir=%s\n",
		result.Checked, result.Upgraded, result.Skipped, result.Failed, installRoot)
	if result.Failed > 0 {
		os.Exit(1)
	}
}

type upgradeResult struct {
	Checked, Upgraded, Skipped, Failed int
}

func extractUpdateURL(config map[string]any, skillDir string) string {
	for _, key := range []string{"update_url", "updateUrl", "upgrade_url", "upgradeUrl", "manifest_url", "manifestUrl"} {
		if v := str(config[key]); v != "" {
			return resolveURI(v, skillDir)
		}
	}
	for _, containerKey := range []string{"update", "upgrade", "autoupdate"} {
		if nested, ok := config[containerKey].(map[string]any); ok {
			for _, key := range []string{"url", "uri", "manifest", "manifest_url"} {
				if v := str(nested[key]); v != "" {
					return resolveURI(v, skillDir)
				}
			}
		}
	}
	return ""
}

func resolveURI(raw, baseDir string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "file" {
		return raw
	}
	abs := filepath.Join(baseDir, raw)
	abs, _ = filepath.Abs(abs)
	return "file://" + filepath.ToSlash(abs)
}

// ---------------------------------------------------------------------------
// Lockfile
// ---------------------------------------------------------------------------

func loadLock(installRoot string) lockfile {
	path := filepath.Join(installRoot, lockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return lockfile{Version: 1, Skills: make(SkillsLock)}
	}
	var lock lockfile
	if json.Unmarshal(data, &lock) != nil {
		return lockfile{Version: 1, Skills: make(SkillsLock)}
	}
	if lock.Skills == nil {
		lock.Skills = make(SkillsLock)
	}
	return lock
}

func saveLock(installRoot string, lock lockfile) {
	os.MkdirAll(installRoot, 0o755)
	path := filepath.Join(installRoot, lockfileName)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "save lockfile: %v\n", err)
		return
	}
	os.WriteFile(path, append(data, '\n'), 0o644)
}

func updateClawhubLock(slug, version string) {
	lockPath := expandUser("~/.chatclaw/openclaw/workspace/.clawhub/lock.json")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	if raw["version"] != float64(1) {
		return
	}
	skills, ok := raw["skills"].(map[string]any)
	if !ok {
		skills = make(map[string]any)
		raw["skills"] = skills
	}
	skills[slug] = map[string]any{
		"version":     version,
		"installedAt": time.Now().UnixMilli(),
	}
	os.MkdirAll(filepath.Dir(lockPath), 0o755)
	out, _ := json.MarshalIndent(raw, "", "  ")
	os.WriteFile(lockPath, append(out, '\n'), 0o644)
}

// ---------------------------------------------------------------------------
// HTTP / file fetching
// ---------------------------------------------------------------------------

func fetchJSON(uri string, timeoutSec int) (map[string]any, error) {
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	var body []byte
	if u.Scheme == "file" || u.Scheme == "" {
		p := u.Path
		if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		body, err = os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("file not found: %s", p)
		}
	} else {
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "skillhub-cli")
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch error: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func downloadFile(uri, dest string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}

	if u.Scheme == "file" || u.Scheme == "" {
		p := u.Path
		if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("local file not found: %s", p)
		}
		return copyFile(p, dest)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "skillhub-cli")
	req.Header.Set("Accept", "application/zip,application/octet-stream,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	return err
}

// ---------------------------------------------------------------------------
// Tar extraction (for CLI self-upgrade, kept for completeness)
// ---------------------------------------------------------------------------

func extractTarGz(archivePath, destination string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := header.Name
		if strings.HasPrefix(name, "./") {
			name = name[2:]
		}
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		targetPath := filepath.Join(destination, parts[1])
		mode := header.FileInfo().Mode()

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(targetPath, 0o755)
		case tar.TypeReg, tar.TypeRegA:
			os.MkdirAll(filepath.Dir(targetPath), 0o755)
			dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(dst, tr)
			dst.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(targetPath), 0o755)
			os.Symlink(header.Linkname, targetPath)
		}
	}
}

// ---------------------------------------------------------------------------
// SHA256
// ---------------------------------------------------------------------------

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func fillTpl(tpl, slug string) string {
	if tpl == "" {
		return ""
	}
	return strings.ReplaceAll(tpl, "{slug}", url.QueryEscape(slug))
}

func versionIsNewer(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" {
		return false
	}
	if current == "" {
		return true
	}

	a := parseVersion(candidate)
	b := parseVersion(current)
	if a == nil && b == nil {
		return candidate != current
	}
	if a == nil || b == nil {
		return candidate != current
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		ai, bi := 0, 0
		if i < len(a) {
			ai = a[i]
		}
		if i < len(b) {
			bi = b[i]
		}
		if ai > bi {
			return true
		}
		if ai < bi {
			return false
		}
	}
	return false
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(strings.ToLower(v), "v")
	v = strings.SplitN(strings.Split(v, "-")[0], "+", 2)[0]
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func cv(v string) string {
	if v == "" {
		return "<unknown>"
	}
	return v
}
