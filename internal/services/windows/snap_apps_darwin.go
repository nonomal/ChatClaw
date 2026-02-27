//go:build darwin

package windows

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func currentDarwinProcessNamesForFilter() map[string]struct{} {
	names := make(map[string]struct{})

	add := func(name string) {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return
		}
		names[normalized] = struct{}{}
		names[strings.TrimSuffix(normalized, filepath.Ext(normalized))] = struct{}{}
	}

	if exePath, err := os.Executable(); err == nil {
		add(filepath.Base(exePath))
	}
	if len(os.Args) > 0 {
		add(filepath.Base(os.Args[0]))
	}

	return names
}

func listRunningApps() ([]SnapAppCandidate, error) {
	script := `
tell application "System Events"
  set outText to ""
  set appList to application processes whose background only is false
  repeat with p in appList
    try
      set appName to name of p
      set isVisible to visible of p
      if isVisible is true then
        set bundleID to ""
        try
          set bundleID to bundle identifier of p
        end try
        set outText to outText & appName & "|" & bundleID & linefeed
      end if
    end try
  end repeat
  return outText
end tell
`
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return []SnapAppCandidate{}, nil
	}

	lines := strings.Split(raw, "\n")
	selfProcessNames := currentDarwinProcessNamesForFilter()
	seen := make(map[string]struct{}, len(lines))
	apps := make([]SnapAppCandidate, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}

		processName := name
		if len(parts) > 1 {
			bundleID := strings.TrimSpace(parts[1])
			if bundleID != "" {
				processName = bundleID
			}
		}

		lower := strings.ToLower(processName)
		nameLower := strings.ToLower(name)
		if _, isSelfProcess := selfProcessNames[lower]; isSelfProcess {
			continue
		}
		if _, isSelfName := selfProcessNames[nameLower]; isSelfName {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}

		apps = append(apps, SnapAppCandidate{
			Name:        name,
			ProcessName: processName,
			Icon:        inferSnapIcon(name, processName),
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	return apps, nil
}
