package main

import (
	"archive/zip"
	"bytes"
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
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Version      = "0.1.0"
	LockfileName = "lock.json"
	DotDir       = ".clawhub"
	OriginFile   = "origin.json"

	DefaultRegistry = "https://cn.clawhub-mirror.com" //官网：https://clawhub.ai

	RouteSearch   = "/api/v1/search"
	RouteResolve  = "/api/v1/resolve"
	RouteDownload = "/api/v1/download"
	RouteSkills   = "/api/v1/skills"
	RouteWhoami   = "/api/v1/whoami"

	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0 (compatible; ClawHub-Go/1.0)"

	DefaultInstallRoot = "~/.chatclaw/openclaw/skills"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Lockfile struct {
	Version int                       `json:"version"`
	Skills  map[string]SkillLockEntry `json:"skills"`
}

type SkillLockEntry struct {
	Version     string `json:"version"`
	InstalledAt int64  `json:"installedAt"`
}

type Origin struct {
	Version          int    `json:"version"`
	Registry         string `json:"registry"`
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installedVersion"`
	InstalledAt      int64  `json:"installedAt"`
}

type Registry struct {
	baseURL    string
	authBase   string
	httpClient *http.Client
}

type SearchResult struct {
	Slug        string  `json:"slug"`
	DisplayName string  `json:"displayName"`
	Summary     string  `json:"summary"`
	Version     string  `json:"version"`
	Score       float64 `json:"score"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type Moderation struct {
	IsSuspicious     bool     `json:"isSuspicious"`
	IsMalwareBlocked bool     `json:"isMalwareBlocked"`
	Verdict          string   `json:"verdict"`
	ReasonCodes      []string `json:"reasonCodes"`
	Summary          string   `json:"summary"`
}

type Owner struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Image       string `json:"image"`
}

type SkillVersion struct {
	Version   string `json:"version"`
	CreatedAt int64  `json:"createdAt"`
	Changelog string `json:"changelog"`
	License   string `json:"license"`
}

type SkillMeta struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Summary     string `json:"summary"`
	Tags        any    `json:"tags"`
	Stats       any    `json:"stats"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type SkillResponse struct {
	Skill         *SkillMeta    `json:"skill"`
	LatestVersion *SkillVersion `json:"latestVersion"`
	Owner         *Owner        `json:"owner"`
	Moderation    *Moderation   `json:"moderation"`
}

type ResolveResponse struct {
	Match         *VersionInfo `json:"match"`
	LatestVersion *VersionInfo `json:"latestVersion"`
}

type VersionInfo struct {
	Version string `json:"version"`
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

var ErrSkillNotFound = errors.New("skill not found")

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: clawhub <command> [args]")
		fmt.Fprintln(os.Stderr, "commands: search, install, list, inspect, update")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "version":
		fmt.Println(Version)
	case "search":
		searchCmd()
	case "install":
		installCmd()
	case "list":
		listCmd()
	case "inspect":
		inspectCmd()
	case "update":
		updateCmd()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func newRegistry(baseURL string) *Registry {
	if baseURL == "" {
		baseURL = DefaultRegistry
	}
	return &Registry{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (r *Registry) BuildURL(path string) *url.URL {
	base := r.baseURL
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return &url.URL{}
	}
	return u
}

func (r *Registry) DownloadURL(slug, version string) *url.URL {
	u := r.BuildURL(RouteDownload)
	q := u.Query()
	q.Set("slug", slug)
	if version != "" {
		q.Set("version", version)
	}
	u.RawQuery = q.Encode()
	return u
}

func (r *Registry) APIRequest(method, path string, token string, queryParams map[string]string) ([]byte, int, error) {
	var u *url.URL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		var err error
		u, err = url.Parse(path)
		if err != nil {
			return nil, 0, err
		}
	} else {
		u = r.BuildURL(path)
	}

	if queryParams != nil {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	return r.doRequestWithRetry(method, u.String(), token)
}

func (r *Registry) doRequestWithRetry(method, urlStr, token string) ([]byte, int, error) {
	maxRetries := 6
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest(method, urlStr, nil)
		if err != nil {
			return nil, 0, err
		}

		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := r.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &HTTPError{StatusCode: resp.StatusCode, Message: string(body)}
			sleepSec := retryAfter(respRetryAfter(resp), attempt)
			fmt.Fprintf(os.Stderr, "Rate limited, retrying in %ds...\n", sleepSec)
			time.Sleep(time.Duration(sleepSec) * time.Second)
			continue
		}

		if resp.StatusCode >= 400 {
			return body, resp.StatusCode, &HTTPError{
				StatusCode: resp.StatusCode,
				Message:    string(body),
			}
		}

		return body, resp.StatusCode, nil
	}

	return nil, 0, lastErr
}

func respRetryAfter(resp *http.Response) int {
	h := resp.Header.Get("Retry-After")
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n > 0 {
		return n
	}
	return 0
}

func retryAfter(retryAfterHeader int, attempt int) int {
	if retryAfterHeader > 0 {
		return retryAfterHeader
	}
	base := (1 << attempt) * 2
	return base + randInt(0, base/2+1)
}

func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + int(time.Now().UnixNano()%int64(max-min))
}

func (r *Registry) DownloadZip(slug, version, token string) ([]byte, error) {
	u := r.DownloadURL(slug, version)

	maxRetries := 6
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			sleepSec := retryAfter(0, attempt)
			fmt.Fprintf(os.Stderr, "Rate limited, retrying in %ds...\n", sleepSec)
			time.Sleep(time.Duration(sleepSec) * time.Second)
		}

		data, err := r.downloadZipOnceWithRetry(u.String(), token)
		if err == nil {
			return data, nil
		}

		if he, ok := err.(*HTTPError); ok && (he.StatusCode == 429 || he.StatusCode >= 500) {
			lastErr = err
			continue
		}
		return nil, err
	}

	return nil, lastErr
}

func (r *Registry) downloadZipOnceWithRetry(urlStr, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/zip,application/octet-stream,*/*")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Message: string(body)}
	}

	return io.ReadAll(resp.Body)
}

func (r *Registry) Search(query string, limit int, token string) (*SearchResponse, error) {
	q := map[string]string{"q": query}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	body, _, err := r.APIRequest("GET", RouteSearch, token, q)
	if err != nil {
		return nil, err
	}
	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (r *Registry) GetSkill(slug, token string) (*SkillResponse, error) {
	path := RouteSkills + "/" + url.PathEscape(slug)
	body, status, err := r.APIRequest("GET", path, token, nil)
	if err != nil {
		if status == 404 {
			return nil, ErrSkillNotFound
		}
		return nil, err
	}
	var resp SkillResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (r *Registry) ResolveSkill(slug, hash, token string) (*ResolveResponse, error) {
	q := map[string]string{"slug": slug}
	if hash != "" {
		q["hash"] = hash
	}
	body, _, err := r.APIRequest("GET", RouteResolve, token, q)
	if err != nil {
		return nil, err
	}
	var resp ResolveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Search command
// ---------------------------------------------------------------------------

func searchCmd() {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: clawhub search <query> [--limit N] [--registry URL]")
	}
	limit := fs.Int("limit", 20, "")
	registry := fs.String("registry", DefaultRegistry, "")
	token := fs.String("token", "", "")
	_ = fs.Parse(os.Args[2:])

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	query := strings.Join(fs.Args(), " ")

	r := newRegistry(*registry)
	resp, err := r.Search(query, *limit, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(resp.Results) == 0 {
		fmt.Println("No results.")
		return
	}
	for _, res := range resp.Results {
		name := res.DisplayName
		if name == "" {
			name = res.Slug
		}
		fmt.Printf("%s  %s  (score: %.3f)\n", res.Slug, name, res.Score)
		if res.Summary != "" {
			fmt.Printf("  %s\n", res.Summary)
		}
		if res.Version != "" {
			fmt.Printf("  version: %s\n", res.Version)
		}
	}
}

// ---------------------------------------------------------------------------
// Install command
// ---------------------------------------------------------------------------

func installCmd() {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: clawhub install <slug> [--registry URL] [--force] [--token TOKEN]")
	}
	registry := fs.String("registry", DefaultRegistry, "")
	token := fs.String("token", "", "")
	force := fs.Bool("force", false, "")
	_ = fs.Parse(os.Args[2:])

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	slug := strings.TrimSpace(fs.Arg(0))
	if strings.ContainsAny(slug, "/\\..") {
		fmt.Fprintf(os.Stderr, "Error: invalid slug: %s\n", slug)
		os.Exit(1)
	}

	workdir := expandUser(DefaultInstallRoot)

	r := newRegistry(*registry)

	fmt.Fprintf(os.Stderr, "Resolving %s...\n", slug)
	meta, err := r.GetSkill(slug, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: fetch skill metadata: %v\n", err)
		os.Exit(1)
	}
	if meta.Skill == nil {
		fmt.Fprintf(os.Stderr, "Error: skill not found: %s\n", slug)
		os.Exit(1)
	}
	slug = meta.Skill.Slug

	if meta.Moderation != nil && meta.Moderation.IsMalwareBlocked {
		fmt.Fprintf(os.Stderr, "Error: skill %s is flagged as malware and cannot be installed\n", slug)
		os.Exit(1)
	}
	if meta.Moderation != nil && meta.Moderation.IsSuspicious && !*force {
		fmt.Fprintf(os.Stderr, "Warning: %s is flagged as suspicious (VirusTotal Code Insight)\n", slug)
		fmt.Fprintf(os.Stderr, "  Reason: %s\n", meta.Moderation.Summary)
		fmt.Fprintf(os.Stderr, "  Use --force to install anyway.\n")
		os.Exit(1)
	}

	version := ""
	if meta.LatestVersion != nil {
		version = meta.LatestVersion.Version
	}
	if version == "" {
		fmt.Fprintf(os.Stderr, "Error: cannot resolve version for %s\n", slug)
		os.Exit(1)
	}

	targetDir := filepath.Join(workdir, slug)
	if !*force {
		if _, err := os.Stat(targetDir); err == nil {
			fmt.Fprintf(os.Stderr, "Error: already installed: %s (use --force to overwrite)\n", targetDir)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "Downloading %s@%s...\n", slug, version)
	zipData, err := r.DownloadZip(slug, version, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: download: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Extracting...\n")
	if err := extractZipToDir(zipData, targetDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: extract: %v\n", err)
		os.Exit(1)
	}

	writeOrigin(targetDir, &Origin{
		Version:          1,
		Registry:         r.baseURL,
		Slug:             slug,
		InstalledVersion: version,
		InstalledAt:      time.Now().UnixMilli(),
	})

	lock, _ := readLockfile(workdir)
	if lock.Skills == nil {
		lock.Skills = make(map[string]SkillLockEntry)
	}
	lock.Skills[slug] = SkillLockEntry{
		Version:     version,
		InstalledAt: time.Now().UnixMilli(),
	}
	writeLockfile(workdir, lock)

	fmt.Printf("Installed %s@%s -> %s\n", slug, version, targetDir)
}

// ---------------------------------------------------------------------------
// List command
// ---------------------------------------------------------------------------

func listCmd() {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fsDir := fs.String("dir", DefaultInstallRoot, "")
	_ = fs.Parse(os.Args[2:])

	workdir := expandUser(*fsDir)
	lock, err := readLockfile(workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read lockfile: %v\n", err)
		os.Exit(1)
	}
	if len(lock.Skills) == 0 {
		fmt.Println("No installed skills.")
		return
	}
	slugs := listInstalledSkills(lock)
	for _, s := range slugs {
		entry := lock.Skills[s]
		version := entry.Version
		if version == "" {
			version = "<unknown>"
		}
		fmt.Printf("%s  %s\n", s, version)
	}
}

// ---------------------------------------------------------------------------
// Inspect command
// ---------------------------------------------------------------------------

func inspectCmd() {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: clawhub inspect <slug> [--registry URL] [--json]")
	}
	registry := fs.String("registry", DefaultRegistry, "")
	token := fs.String("token", "", "")
	jsonFlag := fs.Bool("json", false, "")
	_ = fs.Parse(os.Args[2:])

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	slug := fs.Arg(0)

	r := newRegistry(*registry)
	meta, err := r.GetSkill(slug, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if meta.Skill == nil {
		fmt.Fprintf(os.Stderr, "Error: skill not found: %s\n", slug)
		os.Exit(1)
	}

	if *jsonFlag {
		data, _ := json.MarshalIndent(meta, "", "  ")
		os.Stdout.Write(data)
		return
	}

	skill := meta.Skill
	fmt.Printf("%s  %s\n", skill.Slug, skill.DisplayName)
	if skill.Summary != "" {
		fmt.Printf("Summary: %s\n", skill.Summary)
	}
	if meta.Owner != nil && meta.Owner.Handle != "" {
		fmt.Printf("Owner: %s\n", meta.Owner.Handle)
	}
	if meta.LatestVersion != nil {
		fmt.Printf("Latest: %s\n", meta.LatestVersion.Version)
	}
	fmt.Printf("Created: %s\n", formatTime(skill.CreatedAt))
	fmt.Printf("Updated: %s\n", formatTime(skill.UpdatedAt))

	if meta.Moderation != nil {
		if meta.Moderation.IsMalwareBlocked {
			fmt.Println("Security: BLOCKED (malware)")
		} else if meta.Moderation.IsSuspicious {
			fmt.Println("Security: SUSPICIOUS")
			if meta.Moderation.Summary != "" {
				fmt.Printf("  Reason: %s\n", meta.Moderation.Summary)
			}
		} else {
			fmt.Println("Security: clean")
		}
	}
}

// ---------------------------------------------------------------------------
// Update command
// ---------------------------------------------------------------------------

func updateCmd() {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: clawhub update [slug] [--check-only] [--force] [--registry URL] [--token TOKEN]")
	}
	fsDir := fs.String("dir", DefaultInstallRoot, "")
	checkOnly := fs.Bool("check-only", false, "")
	force := fs.Bool("force", false, "")
	registry := fs.String("registry", DefaultRegistry, "")
	token := fs.String("token", "", "")
	_ = fs.Parse(os.Args[2:])

	workdir := expandUser(*fsDir)
	lock, err := readLockfile(workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read lockfile: %v\n", err)
		os.Exit(1)
	}

	var targets []string
	if fs.NArg() > 0 {
		targets = []string{fs.Arg(0)}
	} else {
		targets = listInstalledSkills(lock)
	}
	if len(targets) == 0 {
		fmt.Println("No skills to update.")
		return
	}

	r := newRegistry(*registry)
	checked, upgraded, skipped, failed := 0, 0, 0, 0

	for _, slug := range targets {
		checked++
		targetDir := filepath.Join(workdir, slug)

		if _, err := os.Stat(targetDir); err != nil {
			fmt.Printf("[%s] skip: not found\n", slug)
			skipped++
			continue
		}

		fingerprint, _ := hashSkillDir(targetDir)

		resolveResp, err := r.ResolveSkill(slug, fingerprint, *token)
		if err != nil {
			fmt.Printf("[%s] fail: %v\n", slug, err)
			failed++
			continue
		}

		matched := ""
		latest := ""
		if resolveResp.Match != nil {
			matched = resolveResp.Match.Version
		}
		if resolveResp.LatestVersion != nil {
			latest = resolveResp.LatestVersion.Version
		}

		curVersion := ""
		if entry, ok := lock.Skills[slug]; ok {
			curVersion = entry.Version
		}

		if !*checkOnly && latest != "" && latest != curVersion && (matched == "" || *force) {
			fmt.Printf("[%s] updating %s -> %s\n", slug, curVersion, latest)

			zipData, err := r.DownloadZip(slug, latest, *token)
			if err != nil {
				fmt.Printf("[%s] fail: download: %v\n", slug, err)
				failed++
				continue
			}

			if err := extractZipToDir(zipData, targetDir); err != nil {
				fmt.Printf("[%s] fail: extract: %v\n", slug, err)
				failed++
				continue
			}

			writeOrigin(targetDir, &Origin{
				Version:          1,
				Slug:             slug,
				InstalledVersion: latest,
				InstalledAt:      time.Now().UnixMilli(),
			})

			lock.Skills[slug] = SkillLockEntry{Version: latest, InstalledAt: time.Now().UnixMilli()}
			upgraded++
			fmt.Printf("[%s] updated -> %s\n", slug, latest)
		} else if matched != "" && matched != curVersion {
			lock.Skills[slug] = SkillLockEntry{Version: matched, InstalledAt: lock.Skills[slug].InstalledAt}
			fmt.Printf("[%s] updated -> %s\n", slug, matched)
			upgraded++
		} else {
			fmt.Printf("[%s] up-to-date (%s)\n", slug, curVersion)
			skipped++
		}
	}

	writeLockfile(workdir, lock)
	fmt.Printf("\nDone: checked=%d upgraded=%d skipped=%d failed=%d\n", checked, upgraded, skipped, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Lockfile
// ---------------------------------------------------------------------------

func readLockfile(workdir string) (*Lockfile, error) {
	path := filepath.Join(workdir, DotDir, LockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Lockfile{Version: 1, Skills: make(map[string]SkillLockEntry)}, nil
		}
		return nil, err
	}
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	if lock.Skills == nil {
		lock.Skills = make(map[string]SkillLockEntry)
	}
	return &lock, nil
}

func writeLockfile(workdir string, lock *Lockfile) error {
	dotDir := filepath.Join(workdir, DotDir)
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dotDir, LockfileName)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func writeOrigin(skillDir string, origin *Origin) error {
	dotDir := filepath.Join(skillDir, DotDir)
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dotDir, OriginFile)
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ---------------------------------------------------------------------------
// Extract zip
// ---------------------------------------------------------------------------

func extractZipToDir(zipBytes []byte, targetDir string) error {
	if err := os.RemoveAll(targetDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("invalid zip: %w", err)
	}

	for _, f := range reader.File {
		name := filepath.FromSlash(f.Name)
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}

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
// Hash skill files
// ---------------------------------------------------------------------------

func hashSkillDir(skillDir string) (string, error) {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return "", err
	}

	var fileHashes []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == DotDir || name == ".clawdhub" || name == "node_modules" || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(skillDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		fileHashes = append(fileHashes, fmt.Sprintf("%s:%s", filepath.ToSlash(name), hex.EncodeToString(h[:])))
	}

	sort.Strings(fileHashes)
	payload := strings.Join(fileHashes, "\n")
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:]), nil
}

// ---------------------------------------------------------------------------
// Utilities
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

func listInstalledSkills(lock *Lockfile) []string {
	slugs := make([]string, 0, len(lock.Skills))
	for slug := range lock.Skills {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

func formatTime(unixMs int64) string {
	if unixMs == 0 {
		return "unknown"
	}
	return time.UnixMilli(unixMs).Format("2006-01-02T15:04:05Z07:00")
}
