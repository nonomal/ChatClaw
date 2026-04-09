package openclawruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"chatclaw/internal/openclaw"

	"github.com/Masterminds/semver/v3"
)

var openClawRegistryURLs = []string{
	"https://registry.npmjs.org",
	"https://registry.npmmirror.com",
}

func (m *Manager) upgradeRuntimeLocked() (*RuntimeUpgradeResult, error) {
	if m.isShuttingDown() {
		return nil, fmt.Errorf("runtime is shutting down")
	}

	m.upgradeInProgress.Store(true)
	defer func() {
		m.upgradeInProgress.Store(false)
		m.upgradeCancelCh = nil
	}()

	m.upgradeMu.Lock()
	defer m.upgradeMu.Unlock()

	// Reset output buffer and set start time.
	m.upgradeOutputBuf.Reset()
	m.upgradeCancelCh = make(chan struct{})
	m.upgradeStartTime = time.Now()

	// Periodically broadcast progress every 2 seconds while upgrade runs.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				m.broadcastUpgradeProgress(-1, "still running...")
			case <-m.upgradeCancelCh:
				return
			}
		}
	}()

	activeBundle, err := resolveBundledRuntime()
	if err != nil {
		return nil, err
	}

	currentVersion, err := verifyInstalled(activeBundle)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	latestVersion, registryURL, err := fetchLatestOpenClawVersion(ctx)
	if err != nil {
		return nil, err
	}

	result := &RuntimeUpgradeResult{
		PreviousVersion: currentVersion,
		CurrentVersion:  currentVersion,
		LatestVersion:   latestVersion,
		RuntimeSource:   activeBundle.Source,
		RuntimePath:     activeBundle.Root,
	}
	if !isRuntimeUpgradeAvailable(currentVersion, latestVersion) {
		return result, nil
	}

	m.broadcastUpgradeProgress(0, fmt.Sprintf("Starting upgrade to openclaw@%s", latestVersion))

	// Check if a staging dir for this version already exists and is complete.
	target := runtime.GOOS + "-" + runtime.GOARCH
	userTargetDir, _ := openclaw.UserRuntimeTargetDir(target)
	candidateStagingDir := filepath.Join(userTargetDir, ".staging-"+sanitizeRuntimeVersion(latestVersion))
	stagingDir := "" // passed to installUserRuntimeOverrideWithCancel; empty = create fresh
	if dirExists, _ := os.Stat(candidateStagingDir); dirExists != nil {
		if isComplete, verifyErr := verifyStagingComplete(candidateStagingDir); verifyErr == nil && isComplete {
			result.HasExistingVersion = true
			result.ExistingVersion = latestVersion
			// Return early so frontend can offer Continue / Restart options.
			return result, nil
		}
	}

	// No existing staging — proceed with normal upgrade.
	m.closeClient()
	m.stopProcess()

	// Kill stray node processes that may lock runtime directories before any
	// file operations (backup/rename/remove). Non-fatal if it fails.
	_ = killAllNodeProcesses()

	// Fast path: if the user runtime current directory already contains the target
	// version, skip downloading/building and just activate it directly.
	currentDir, err := openclaw.UserRuntimeCurrentDir(target)
	if err == nil {
		if skip, _ := checkUserRuntimeAlreadyHasVersion(currentDir, latestVersion); skip {
			m.app.Logger.Info("openclaw: runtime already at latest version, activating",
				"version", latestVersion, "dir", currentDir)
			if err := m.reconcileLocked(false); err != nil {
				return nil, fmt.Errorf("activate existing runtime: %w", err)
			}
			status := m.GetStatus()
			result.Upgraded = true
			result.CurrentVersion = latestVersion
			result.RuntimeSource = status.RuntimeSource
			result.RuntimePath = status.RuntimePath
			return result, nil
		}
	}

	installResult, err := m.installUserRuntimeOverrideWithCancel(activeBundle, latestVersion, registryURL, stagingDir)
	if err != nil {
		// Install failed (npm download or staging). No .current dir was created, no .backup touched.
		// Try to recover: attempt reconcile once (uses embedded or OSS).
		m.app.Logger.Error("openclaw: install failed, attempting recovery", "error", err)
		m.broadcastUpgradeProgress(0, "Installation failed, attempting recovery...")
		if reconcileErr := m.reconcileLocked(false); reconcileErr != nil {
			m.app.Logger.Error("openclaw: recovery reconcile failed", "error", reconcileErr)
		}
		return nil, err
	}

	// Activation succeeded (currentDir now points to the new version).
	// Try starting the gateway up to 5 times before declaring the upgrade a failure.
	const maxStartAttempts = 5
	var startupErr error
	for attempt := 1; attempt <= maxStartAttempts; attempt++ {
		select {
		case <-m.upgradeCancelCh:
			m.app.Logger.Info("openclaw: upgrade cancelled during gateway start")
			m.broadcastUpgradeProgress(0, "Upgrade cancelled, rolling back...")
			if installResult.Restore != nil {
				_ = installResult.Restore()
			}
			_ = m.reconcileLocked(false)
			return nil, fmt.Errorf("upgrade cancelled")
		default:
		}

		m.broadcastUpgradeProgress(90, fmt.Sprintf("Starting gateway (attempt %d/%d)...", attempt, maxStartAttempts))

		// If the port is already in use, the gateway is already running from a previous
		// startup. This is normal on fresh app launch — skip the port check and let
		// reconcileLocked figure out the state.
		port := m.store.Get().GatewayPort
		if isPortAvailable(port) {
			if reconcileErr := m.reconcileLocked(false); reconcileErr == nil {
				startupErr = nil
				goto upgradeSucceeded
			} else {
				startupErr = reconcileErr
				m.app.Logger.Warn("openclaw: gateway start attempt failed",
					"attempt", attempt, "maxAttempts", maxStartAttempts, "error", startupErr)
				if attempt == maxStartAttempts {
					break
				}
				if !installResult.HadCurrent {
					m.broadcastUpgradeProgress(0, fmt.Sprintf("Gateway failed (attempt %d/%d), running diagnostic...", attempt, maxStartAttempts))
					if _, fixErr := m.RunDoctorCommand("check", true); fixErr != nil {
						m.app.Logger.Warn("openclaw: doctor fix failed", "error", fixErr)
					}
				}
				time.Sleep(2 * time.Second)
			}
		} else {
			// Port is already in use — gateway is already running, proceed directly.
			m.app.Logger.Info("openclaw: gateway already running, skipping start",
				"port", port, "attempt", attempt)
			m.broadcastUpgradeProgress(100, "Gateway already running")
			goto upgradeSucceeded
		}
	}

	// All 5 attempts failed.
	if installResult.HadCurrent {
		m.app.Logger.Error("openclaw: gateway failed after 5 attempts, rolling back",
			"error", startupErr)
		m.broadcastUpgradeProgress(0, "Gateway failed after 5 attempts, rolling back to previous version...")
		if rollbackErr := installResult.Restore(); rollbackErr != nil {
			m.app.Logger.Error("openclaw: rollback failed", "error", rollbackErr)
		}
		time.Sleep(500 * time.Millisecond)
		_ = m.reconcileLocked(false)
	} else {
		m.app.Logger.Error("openclaw: first-install gateway failed after 5 attempts",
			"error", startupErr)
		m.broadcastUpgradeProgress(0, "OpenClaw failed to start after 5 attempts, please run openclaw doctor manually.")
		_ = m.reconcileLocked(false)
	}
	return nil, fmt.Errorf("gateway failed after %d attempts: %w", maxStartAttempts, startupErr)

upgradeSucceeded:

	// Gateway started successfully. Now clean up old staging dirs and the safety backup.
	// .backup is only safe to delete now because the gateway is running with the new version.
	installResult.DeleteBackup()
	installResult.Cleanup()

	status := m.GetStatus()
	result.Upgraded = true
	result.CurrentVersion = latestVersion
	result.RuntimeSource = status.RuntimeSource
	result.RuntimePath = status.RuntimePath
	return result, nil
}

// FetchLatestOpenClawPackageVersion returns the latest openclaw npm package version (read-only).
func FetchLatestOpenClawPackageVersion(ctx context.Context) (string, error) {
	v, _, err := fetchLatestOpenClawVersion(ctx)
	return v, err
}

// IsOpenClawCLIUpgradeAvailable reports whether latest is newer than current (semver when possible).
func IsOpenClawCLIUpgradeAvailable(currentVersion, latestVersion string) bool {
	return isRuntimeUpgradeAvailable(currentVersion, latestVersion)
}

func isRuntimeUpgradeAvailable(currentVersion, latestVersion string) bool {
	current, err := semver.NewVersion(strings.TrimSpace(currentVersion))
	if err != nil {
		return strings.TrimSpace(currentVersion) != strings.TrimSpace(latestVersion) &&
			strings.TrimSpace(latestVersion) != ""
	}
	latest, err := semver.NewVersion(strings.TrimSpace(latestVersion))
	if err != nil {
		return strings.TrimSpace(currentVersion) != strings.TrimSpace(latestVersion) &&
			strings.TrimSpace(latestVersion) != ""
	}
	return latest.GreaterThan(current)
}

func fetchLatestOpenClawVersion(ctx context.Context) (string, string, error) {
	var issues []string
	for _, registryURL := range openClawRegistryURLs {
		version, err := fetchLatestOpenClawVersionFromRegistry(ctx, registryURL)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", registryURL, err))
			continue
		}
		return version, strings.TrimRight(registryURL, "/"), nil
	}

	if len(issues) == 0 {
		return "", "", errors.New("fetch OpenClaw version failed")
	}
	return "", "", fmt.Errorf("fetch latest OpenClaw version failed: %s", strings.Join(issues, "; "))
}

func fetchLatestOpenClawVersionFromRegistry(ctx context.Context, registryURL string) (string, error) {
	type latestPayload struct {
		Version string `json:"version"`
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(registryURL, "/")+"/openclaw/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}

	var payload latestPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode latest payload: %w", err)
	}
	payload.Version = strings.TrimSpace(payload.Version)
	if payload.Version == "" {
		return "", errors.New("missing version")
	}
	return payload.Version, nil
}

// upgradeProgress is called to broadcast real-time progress updates to the UI.
// progress is 0-100, message is a human-readable step description.
type upgradeProgress func(progress int, message string)

// userRuntimeOverrideResult is returned by installUserRuntimeOverride to give the caller
// full control over rollback, cleanup, and retry decisions.
type userRuntimeOverrideResult struct {
	Staged       *bundledRuntime // the newly installed runtime; Root points to currentDir after activation
	HadCurrent   bool            // true if a pre-existing runtime was backed up to .backup (rollback possible)
	Restore      func() error     // rollback: delete currentDir, rename .backup → currentDir. Idempotent (no-op if .backup absent)
	Cleanup      func()           // delete stale .staging-* dirs. Does NOT delete .backup.
	DeleteBackup func()          // delete .backup — only safe to call after upgrade succeeds and gateway is running.
}

// installUserRuntimeOverrideWithCancel installs the given openclaw version and activates it.
// - cancelCh: closed by CancelUpgrade to abort the operation mid-flight.
// - stagingDirHint: if non-empty, this exact path is used instead of computing targetStagingDir.
//   Used by ContinueUpgrade where the staging dir already exists and is known.
// - forceNpmInstall: if true, always runs npm install even if staging dir appears complete.
//   This ensures partial installs are caught and repaired before activation.
func (m *Manager) installUserRuntimeOverrideWithCancel(
	activeBundle *bundledRuntime,
	version, registryURL string,
	stagingDirHint string,
) (*userRuntimeOverrideResult, error) {
	target := runtime.GOOS + "-" + runtime.GOARCH
	userTargetDir, err := openclaw.UserRuntimeTargetDir(target)
	if err != nil {
		return nil, err
	}
	currentDir, err := openclaw.UserRuntimeCurrentDir(target)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(userTargetDir, 0o700); err != nil {
		return nil, fmt.Errorf("create user runtime dir: %w", err)
	}

	var targetStagingDir string
	if stagingDirHint != "" {
		targetStagingDir = stagingDirHint
	} else {
		targetStagingDir = filepath.Join(userTargetDir, ".staging-"+sanitizeRuntimeVersion(version))
	}
	backupDir := filepath.Join(userTargetDir, ".backup")
	result := &userRuntimeOverrideResult{DeleteBackup: func() {}}

	onProgress := func(progress int, msg string) {
		m.broadcastUpgradeProgress(progress, msg)
		m.appendUpgradeOutput(msg)
	}

	// Always run npm install for openclaw package to catch partial installs.
	// Even when staging dir already has all files, re-install to ensure node_modules are intact.
	// This is the "forceNpmInstall" behavior required by requirement 3.
	needsNpmInstall := true

	if stagingDirHint == "" {
		// Fresh upgrade path: staging dir may or may not exist.
		// We still run npm install (needsNpmInstall = true).
		_ = os.RemoveAll(targetStagingDir)
		if err := os.MkdirAll(targetStagingDir, 0o755); err != nil {
			return nil, fmt.Errorf("create staging runtime dir: %w", err)
		}
	} else {
		// Continue/Resume path: staging dir is given, ensure it exists.
		dirInfo, statErr := os.Stat(targetStagingDir)
		if statErr != nil || !dirInfo.IsDir() {
			return nil, fmt.Errorf("staging directory not found: %s", targetStagingDir)
		}
	}

	stagingDir := targetStagingDir

	onProgress(5, fmt.Sprintf("Copying Node.js runtime for %s", version))
	if err := copyDirRecursive(filepath.Join(activeBundle.Root, "tools", "node"), filepath.Join(stagingDir, "tools", "node")); err != nil {
		_ = os.RemoveAll(targetStagingDir)
		return nil, fmt.Errorf("copy bundled node: %w", err)
	}

	select {
	case <-m.upgradeCancelCh:
		_ = os.RemoveAll(targetStagingDir)
		return nil, fmt.Errorf("upgrade cancelled")
	default:
	}

	npmPrefix := npmGlobalInstallPrefix(stagingDir, runtime.GOOS)
	if runtime.GOOS == "windows" {
		if err := os.MkdirAll(npmPrefix, 0o755); err != nil {
			_ = os.RemoveAll(targetStagingDir)
			return nil, fmt.Errorf("create npm prefix dir: %w", err)
		}
	}

	if needsNpmInstall {
		onProgress(10, fmt.Sprintf("Downloading openclaw@%s from registry", version))
		if stagingDirHint != "" {
			// Re-download from registry (continue path).
			// If registryURL is empty (shouldn't happen), use default.
			if registryURL == "" {
				registryURL = "https://registry.npmjs.org"
			}
			if err := installOpenClawPackageWithCancel(m, activeBundle.Root, version, registryURL, npmPrefix, onProgress); err != nil {
				// Keep staging intact: it contains the bundled node runtime and
				// partial openclaw package. The next ContinueUpgrade attempt will
				// re-run npm install which overwrites the partial node_modules.
				return nil, err
			}
		} else {
			if err := installOpenClawPackageWithCancel(m, activeBundle.Root, version, registryURL, npmPrefix, onProgress); err != nil {
				// Same as above: keep staging so the next attempt can re-run npm install.
				return nil, err
			}
		}
		onProgress(40, "Verifying installation")
		if err := verifyOpenClawLibLayout(stagingDir); err != nil {
			_ = os.RemoveAll(targetStagingDir)
			return nil, err
		}
		if err := os.MkdirAll(filepath.Join(stagingDir, "bin"), 0o755); err != nil {
			_ = os.RemoveAll(targetStagingDir)
			return nil, fmt.Errorf("create runtime bin dir: %w", err)
		}
		if err := writeCLIWrappers(stagingDir, runtime.GOOS); err != nil {
			_ = os.RemoveAll(targetStagingDir)
			return nil, fmt.Errorf("write runtime CLI wrappers: %w", err)
		}
		if err := writeRuntimeManifest(filepath.Join(stagingDir, "manifest.json"), bundledRuntimeManifest{
			OpenClawVersion: version,
			NodeVersion:     activeBundle.Manifest.NodeVersion,
			Platform:        runtime.GOOS,
			Arch:            runtime.GOARCH,
		}); err != nil {
			_ = os.RemoveAll(targetStagingDir)
			return nil, fmt.Errorf("write runtime manifest: %w", err)
		}
		onProgress(50, "Staging directory ready")
	} else {
		// This branch is now unused since needsNpmInstall is always true,
		// but kept for compile safety in case logic changes.
		onProgress(5, "Reusing cached staging directory")
	}

	select {
	case <-m.upgradeCancelCh:
		_ = os.RemoveAll(targetStagingDir)
		return nil, fmt.Errorf("upgrade cancelled")
	default:
	}

	stagedBundle, err := loadBundledRuntimeCandidate(
		activeBundle.StateDir,
		runtime.GOOS,
		runtime.GOARCH,
		runtimeCandidate{Root: stagingDir, Source: runtimeSourceUser},
	)
	if err != nil {
		return nil, err
	}

	// If user already has this exact version (current dir), skip the install and activate directly.
	// This avoids touching currentDir at all, eliminating any risk of rename failures.
	if skipInstall, err := checkUserRuntimeAlreadyHasVersion(currentDir, version); err == nil && skipInstall {
		stagedBundle.Root = currentDir
		stagedBundle.CLIPath = filepath.Join(currentDir, "bin", cliName())
		if runtime.GOOS == "windows" {
			stagedBundle.NodeExePath = filepath.Join(currentDir, "tools", "node", "node.exe")
		} else {
			stagedBundle.NodeExePath = filepath.Join(currentDir, "tools", "node", "bin", "node")
		}
		stagedBundle.Source = runtimeSourceUser
		result.Staged = stagedBundle
		result.HadCurrent = false // not a real upgrade, just switching active version
		result.Cleanup = func() { cleanOldStagingDirs(userTargetDir, version) }
		return result, nil
	}

	if _, err := verifyInstalled(stagedBundle); err != nil {
		return nil, fmt.Errorf("verify staged runtime: %w", err)
	}

	// Phase 1 — backup: must happen BEFORE any rename of current.
	// Kill node processes first so their file locks don't block the backup rename.
	hadCurrent := false
	onProgress(55, "Backing up current runtime")
	if _, err := os.Stat(currentDir); err == nil {
		hadCurrent = true
		_ = killNodeProcessesHoldingRuntimeDir(currentDir)
		time.Sleep(500 * time.Millisecond)
		_ = os.RemoveAll(backupDir)
		if err := os.Rename(currentDir, backupDir); err != nil {
			return nil, fmt.Errorf("backup current runtime: %w", err)
		}
	}
	result.HadCurrent = hadCurrent

	// Phase 2 — activate: rename staging → current.
	// If rename fails, node.exe is still holding a lock; kill and retry once.
	onProgress(60, "Activating new runtime")
	if err := os.Rename(stagingDir, currentDir); err != nil {
		_ = killNodeProcessesHoldingRuntimeDir(currentDir)
		time.Sleep(500 * time.Millisecond)
		if retryErr := os.Rename(stagingDir, currentDir); retryErr != nil {
			// Restore backup on failure.
			if hadCurrent {
				_ = killNodeProcessesHoldingRuntimeDir(backupDir)
				time.Sleep(200 * time.Millisecond)
				_ = os.Rename(backupDir, currentDir)
			}
			return nil, fmt.Errorf("activate upgraded runtime after retry: %w", retryErr)
		}
	}
	onProgress(80, "Verifying new runtime")

	result.Restore = func() error {
		_ = killNodeProcessesHoldingRuntimeDir(currentDir)
		time.Sleep(500 * time.Millisecond)
		_ = killNodeProcessesHoldingRuntimeDir(backupDir)
		time.Sleep(200 * time.Millisecond)
		// Undo the two renames in reverse order.
		// 1. current (new version, e.g. .staging-2026.4.8) → staging name, so it can be re-used.
		if err := os.Rename(currentDir, stagingDir); err != nil {
			// Fallback: if rename fails (e.g. cross-volume), remove and re-create.
			_ = os.RemoveAll(currentDir)
			return fmt.Errorf("rename current to staging during restore: %w", err)
		}
		if !hadCurrent {
			return nil
		}
		// 2. backup (old version) → current, completing the rollback.
		if err := os.Rename(backupDir, currentDir); err != nil {
			return fmt.Errorf("rename backup to current during restore: %w", err)
		}
		return nil
	}
	// cleanup only removes old staging dirs — never .backup.
	result.Cleanup = func() { cleanOldStagingDirs(userTargetDir, version) }
	// Only delete .backup after the upgrade is fully confirmed (gateway is running).
	result.DeleteBackup = func() { _ = os.RemoveAll(backupDir) }

	stagedBundle.Root = currentDir
	stagedBundle.CLIPath = filepath.Join(currentDir, "bin", cliName())
	if runtime.GOOS == "windows" {
		stagedBundle.NodeExePath = filepath.Join(currentDir, "tools", "node", "node.exe")
	} else {
		stagedBundle.NodeExePath = filepath.Join(currentDir, "tools", "node", "bin", "node")
	}
	stagedBundle.Source = runtimeSourceUser
	result.Staged = stagedBundle
	return result, nil
}

// cleanOldStagingDirs removes staging directories for versions other than current.
// It intentionally skips .backup and the directory for the given version.
func cleanOldStagingDirs(userTargetDir, currentVersion string) {
	entries, err := os.ReadDir(userTargetDir)
	if err != nil {
		return
	}
	currentStagingName := ".staging-" + sanitizeRuntimeVersion(currentVersion)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".staging-") {
			continue
		}
		// Keep the current version's staging dir for reuse.
		if entry.Name() == currentStagingName {
			continue
		}
		_ = os.RemoveAll(filepath.Join(userTargetDir, entry.Name()))
	}
}

func installOpenClawPackage(bundleRoot, version, registryURL, npmPrefix string, onProgress upgradeProgress) error {
	npmPath := bundledNpmPath(bundleRoot)
	if _, err := os.Stat(npmPath); err != nil {
		return fmt.Errorf("bundled npm is missing at %s: %w", npmPath, err)
	}

	args := []string{
		"install", "-g",
		"--prefix", npmPrefix,
		"--loglevel", "warn",
		"--no-fund", "--no-audit",
		"--progress",
		"--registry", registryURL,
		"openclaw@" + version,
	}
	cmd := exec.Command(npmPath, args...)
	cmd.Env = buildBundledNodeEnv(bundleRoot)
	setCmdHideWindow(cmd)

	// Stream npm stdout so the UI sees download progress lines.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout for npm install: %w", err)
	}
	// Preserve stderr for error reporting; npm uses it for warnings and errors.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open stderr for npm install: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start npm install: %w", err)
	}

	var stderrLines []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		// npm stderr lines are typically short; bump scanner buffer.
		// npm prepends progress bars to stdout, not stderr, but capture stderr just in case.
		for sc.Scan() {
			stderrLines = append(stderrLines, sc.Text())
		}
	}()

	// Stream stdout: each non-empty line is forwarded as a progress update.
	// Progress is estimated as npm install is not seekable; map 10→90% of the download phase.
	const progressStart, progressEnd = 10, 90
	cur := progressStart
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		onProgress(cur, line)
		// Advance progress in small increments; npm update phases roughly fill 10% each.
		if cur < progressEnd {
			cur += 5
		}
	}
	wg.Wait()
	scanErr := sc.Err()
	waitErr := cmd.Wait()

	if waitErr != nil || scanErr != nil {
		errMsg := strings.TrimSpace(strings.Join(stderrLines, "\n"))
		if errMsg != "" {
			return fmt.Errorf("install openclaw@%s: %w\n%s", version, waitErr, errMsg)
		}
		return fmt.Errorf("install openclaw@%s: %w", version, waitErr)
	}
	return nil
}

// installOpenClawPackageWithCancel wraps installOpenClawPackage and checks cancelCh between npm output lines.
// If cancelCh is closed, it kills the npm process and returns an error.
func installOpenClawPackageWithCancel(m *Manager, bundleRoot, version, registryURL, npmPrefix string, onProgress upgradeProgress) error {
	npmPath := bundledNpmPath(bundleRoot)
	if _, err := os.Stat(npmPath); err != nil {
		return fmt.Errorf("bundled npm is missing at %s: %w", npmPath, err)
	}

	args := []string{
		"install", "-g",
		"--prefix", npmPrefix,
		"--loglevel", "warn",
		"--no-fund", "--no-audit",
		"--progress",
		"--registry", registryURL,
		"openclaw@" + version,
	}
	cmd := exec.Command(npmPath, args...)
	cmd.Env = buildBundledNodeEnv(bundleRoot)
	setCmdHideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout for npm install: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open stderr for npm install: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start npm install: %w", err)
	}

	var stderrLines []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			stderrLines = append(stderrLines, sc.Text())
		}
	}()

	const progressStart, progressEnd = 10, 90
	cur := progressStart
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		select {
		case <-m.upgradeCancelCh:
			_ = cmd.Process.Kill()
			wg.Wait()
			return fmt.Errorf("upgrade cancelled")
		default:
		}

		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		onProgress(cur, line)
		if cur < progressEnd {
			cur += 5
		}
	}
	wg.Wait()
	scanErr := sc.Err()
	waitErr := cmd.Wait()

	if waitErr != nil || scanErr != nil {
		errMsg := strings.TrimSpace(strings.Join(stderrLines, "\n"))
		if errMsg != "" {
			return fmt.Errorf("install openclaw@%s: %w\n%s", version, waitErr, errMsg)
		}
		return fmt.Errorf("install openclaw@%s: %w", version, waitErr)
	}
	return nil
}

func buildBundledNodeEnv(bundleRoot string) []string {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		if k, v, ok := strings.Cut(entry, "="); ok {
			envMap[k] = v
		}
	}
	envMap["SHARP_IGNORE_GLOBAL_LIBVIPS"] = "1"
	envMap["NODE_LLAMA_CPP_SKIP_DOWNLOAD"] = "1"
	envMap["NPM_CONFIG_LOGLEVEL"] = "error"
	envMap["NPM_CONFIG_FUND"] = "false"
	envMap["NPM_CONFIG_AUDIT"] = "false"
	envMap["OPENCLAW_EMBEDDED_IN"] = "ChatClaw"

	var pathKey, nodeBin string
	if runtime.GOOS == "windows" {
		pathKey, nodeBin = "Path", filepath.Join(bundleRoot, "tools", "node")
	} else {
		pathKey, nodeBin = "PATH", filepath.Join(bundleRoot, "tools", "node", "bin")
	}
	if cur := envMap[pathKey]; cur != "" {
		envMap[pathKey] = nodeBin + string(os.PathListSeparator) + cur
	} else {
		envMap[pathKey] = nodeBin
	}

	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	return env
}

func bundledNpmPath(bundleRoot string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(bundleRoot, "tools", "node", "npm.cmd")
	}
	return filepath.Join(bundleRoot, "tools", "node", "bin", "npm")
}

func writeRuntimeManifest(path string, manifest bundledRuntimeManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func sanitizeRuntimeVersion(version string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(version) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "unknown"
	}
	return out
}

// npmGlobalInstallPrefix matches the bundle builder layout.
// Unix npm uses prefix/lib/node_modules, while Windows uses prefix/node_modules.
func npmGlobalInstallPrefix(outputDir string, goos string) string {
	if goos == "windows" {
		return filepath.Join(outputDir, "lib")
	}
	return outputDir
}

func verifyOpenClawLibLayout(outputDir string) error {
	pkg := filepath.Join(outputDir, "lib", "node_modules", "openclaw", "package.json")
	if _, err := os.Stat(pkg); err != nil {
		return fmt.Errorf("openclaw package missing at %s after npm install", pkg)
	}
	return nil
}

func writeCLIWrappers(outputDir, goos string) error {
	if goos == "windows" {
		content := strings.Join([]string{
			`@echo off`,
			`setlocal EnableExtensions`,
			`set "SCRIPT_DIR=%~dp0"`,
			`set "OPENCLAW_EMBEDDED_IN=ChatClaw"`,
			// Directly invoke node.exe — no intermediate cmd.exe layer.
			// The wrapper is provided for manual CLI usage (developer terminal).
			// startProcess in manager.go bypasses this and calls node.exe directly
			// so that CREATE_NO_WINDOW takes effect on node.exe itself.
			`"%SCRIPT_DIR%..\tools\node\node.exe" "%SCRIPT_DIR%..\lib\node_modules\openclaw\dist\entry.js" %*`,
			"",
		}, "\r\n")
		return os.WriteFile(filepath.Join(outputDir, "bin", "openclaw.cmd"), []byte(content), 0o644)
	}

	content := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		`SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"`,
		`export OPENCLAW_EMBEDDED_IN="ChatClaw"`,
		`exec "$SCRIPT_DIR/../tools/node/bin/node" "$SCRIPT_DIR/../lib/node_modules/openclaw/dist/entry.js" "$@"`,
		"",
	}, "\n")
	path := filepath.Join(outputDir, "bin", "openclaw")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

func copyDirRecursive(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		switch mode := info.Mode(); {
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return err
			}
		case info.IsDir():
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		default:
			if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// killNodeProcessesHoldingRuntimeDir finds and kills all node.exe processes whose
// command line references the given runtime directory. This is needed because the
// OpenClaw gateway may fork child node processes (agents, sandboxes) that survive
// the gateway exit and hold file locks on the runtime directory, blocking the
// os.Rename that activates the upgraded staging directory.
// killAllNodeProcesses kills all node.exe processes on Windows.
// Best-effort: errors are logged but not returned, as this is a cleanup safeguard.
func killAllNodeProcesses() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "taskkill",
		"/F", // force kill
		"/FI", "IMAGENAME eq node.exe",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" && !strings.Contains(msg, "no tasks") && !strings.Contains(msg, "not found") {
			return fmt.Errorf("kill node processes: %w: %s", err, msg)
		}
	}
	return nil
}

// killNodeProcessesHoldingRuntimeDir is a legacy alias kept for callers that
// pass a directory argument for logging purposes.
func killNodeProcessesHoldingRuntimeDir(runtimeDir string) error {
	_ = killAllNodeProcesses()
	return nil
}

// checkUserRuntimeAlreadyHasVersion returns true if currentDir already contains
// the same openclaw version that we are trying to install. This avoids a redundant
// npm install when the user path already has the desired version (e.g. it was
// installed via OSS install earlier, or a previous upgrade left it there).
func checkUserRuntimeAlreadyHasVersion(currentDir, version string) (bool, error) {
	manifestPath := filepath.Join(currentDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false, nil // currentDir may not exist or manifest missing
	}
	var manifest bundledRuntimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, nil
	}
	if manifest.OpenClawVersion == version {
		cliPath := filepath.Join(currentDir, "bin", cliName())
		if _, err := os.Stat(cliPath); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// verifyStagingComplete checks whether the staging directory already contains
// a complete openclaw installation and can be reused without rebuilding.
// Returns (true, nil) if it is complete; (false, err) otherwise.
func verifyStagingComplete(stagingDir string) (bool, error) {
	// Check critical files exist
	required := []string{
		filepath.Join(stagingDir, "bin", cliName()),
		filepath.Join(stagingDir, "manifest.json"),
		filepath.Join(stagingDir, "tools", "node"),
		filepath.Join(stagingDir, "lib", "node_modules", "openclaw", "package.json"),
	}
	for _, p := range required {
		if _, err := os.Stat(p); err != nil {
			return false, err
		}
	}
	return true, nil
}

// isPortAvailable reports whether the given TCP port is free to bind.
func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		ln.Close()
		return true
	}
	return false
}
