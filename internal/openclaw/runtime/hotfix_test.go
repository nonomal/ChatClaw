package openclawruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyBundledRuntimeHotfixes_PatchesThinkingAndWhatsappEcho(t *testing.T) {
	root := t.TempDir()
	distDir := filepath.Join(root, "lib", "node_modules", "openclaw", "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("mkdir dist dir: %v", err)
	}

	authPath := filepath.Join(distDir, "auth-profiles-test.js")
	authContent := strings.Join([]string{
		openClawThinkingStreamPatchNeedle1,
		openClawThinkingStreamPatchNeedle2,
		openClawThinkingStreamPatchNeedle3,
		openClawFallbackRetryPromptPatchNeedle,
	}, "\n")
	if err := os.WriteFile(authPath, []byte(authContent), 0o644); err != nil {
		t.Fatalf("write auth fixture: %v", err)
	}

	whatsappPath := filepath.Join(distDir, "channel.runtime-test.js")
	if err := os.WriteFile(whatsappPath, []byte(openClawWhatsappDirectEchoPatchNeedle), 0o644); err != nil {
		t.Fatalf("write whatsapp fixture: %v", err)
	}

	patched, err := applyBundledRuntimeHotfixes(&bundledRuntime{Root: root})
	if err != nil {
		t.Fatalf("apply hotfixes: %v", err)
	}
	if patched != 2 {
		t.Fatalf("patched files = %d, want 2", patched)
	}

	authRaw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read patched auth fixture: %v", err)
	}
	authPatched := string(authRaw)
	for _, want := range []string{
		openClawThinkingStreamPatchValue1,
		openClawThinkingStreamPatchValue2,
		openClawThinkingStreamPatchValue3,
		openClawFallbackRetryPromptPatchValue,
	} {
		if !strings.Contains(authPatched, want) {
			t.Fatalf("patched auth fixture missing %q", want)
		}
	}

	whatsappRaw, err := os.ReadFile(whatsappPath)
	if err != nil {
		t.Fatalf("read patched whatsapp fixture: %v", err)
	}
	if got := string(whatsappRaw); !strings.Contains(got, openClawWhatsappDirectEchoPatchValue) {
		t.Fatalf("patched whatsapp fixture missing direct echo fix")
	}

	patched, err = applyBundledRuntimeHotfixes(&bundledRuntime{Root: root})
	if err != nil {
		t.Fatalf("reapply hotfixes: %v", err)
	}
	if patched != 0 {
		t.Fatalf("patched files on second run = %d, want 0", patched)
	}
}
