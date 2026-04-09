package openclawchannels

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStalePluginInstallStageDirs(t *testing.T) {
	extensionsDir := filepath.Join(t.TempDir(), "extensions")
	installedDir := filepath.Join(extensionsDir, "openclaw-weixin")
	if err := os.MkdirAll(installedDir, 0o755); err != nil {
		t.Fatalf("mkdir installed dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(installedDir, wechatPluginPackageManifestName),
		[]byte(`{"name":"`+wechatPluginPackageName+`"}`),
		0o644,
	); err != nil {
		t.Fatalf("write installed package.json: %v", err)
	}

	matchingStageDir := filepath.Join(extensionsDir, wechatInstallStagePrefix+"match")
	if err := os.MkdirAll(matchingStageDir, 0o755); err != nil {
		t.Fatalf("mkdir matching stage dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(matchingStageDir, wechatPluginPackageManifestName),
		[]byte(`{"name":"`+wechatPluginPackageName+`"}`),
		0o644,
	); err != nil {
		t.Fatalf("write matching stage package.json: %v", err)
	}

	unrelatedStageDir := filepath.Join(extensionsDir, wechatInstallStagePrefix+"other")
	if err := os.MkdirAll(unrelatedStageDir, 0o755); err != nil {
		t.Fatalf("mkdir unrelated stage dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(unrelatedStageDir, wechatPluginPackageManifestName),
		[]byte(`{"name":"@example/other-plugin"}`),
		0o644,
	); err != nil {
		t.Fatalf("write unrelated stage package.json: %v", err)
	}

	removed, err := removeStalePluginInstallStageDirs(extensionsDir, installedDir, wechatPluginPackageName)
	if err != nil {
		t.Fatalf("removeStalePluginInstallStageDirs() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removeStalePluginInstallStageDirs() removed = %d, want 1", removed)
	}
	if _, err := os.Stat(matchingStageDir); !os.IsNotExist(err) {
		t.Fatalf("matching stage dir still exists, stat err = %v", err)
	}
	if _, err := os.Stat(unrelatedStageDir); err != nil {
		t.Fatalf("unrelated stage dir removed unexpectedly: %v", err)
	}
}

func TestRemoveStalePluginInstallStageDirsSkipsWhenInstalledPluginMissing(t *testing.T) {
	extensionsDir := filepath.Join(t.TempDir(), "extensions")
	if err := os.MkdirAll(extensionsDir, 0o755); err != nil {
		t.Fatalf("mkdir extensions dir: %v", err)
	}

	stageDir := filepath.Join(extensionsDir, wechatInstallStagePrefix+"match")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("mkdir stage dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(stageDir, wechatPluginPackageManifestName),
		[]byte(`{"name":"`+wechatPluginPackageName+`"}`),
		0o644,
	); err != nil {
		t.Fatalf("write stage package.json: %v", err)
	}

	removed, err := removeStalePluginInstallStageDirs(extensionsDir, filepath.Join(extensionsDir, "openclaw-weixin"), wechatPluginPackageName)
	if err != nil {
		t.Fatalf("removeStalePluginInstallStageDirs() error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removeStalePluginInstallStageDirs() removed = %d, want 0", removed)
	}
	if _, err := os.Stat(stageDir); err != nil {
		t.Fatalf("stage dir removed unexpectedly: %v", err)
	}
}
