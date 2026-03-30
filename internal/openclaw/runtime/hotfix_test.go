package openclawruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyOpenClawThinkingStreamHotfixFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth-profiles-test.js")
	original := strings.Join([]string{
		openClawThinkingStreamPatchNeedle1,
		openClawThinkingStreamPatchNeedle2,
		openClawThinkingStreamPatchNeedle3,
		openClawFallbackRetryPromptPatchNeedle,
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	changed, err := applyOpenClawThinkingStreamHotfixFile(path)
	if err != nil {
		t.Fatalf("apply hotfix: %v", err)
	}
	if !changed {
		t.Fatalf("expected hotfix to change file")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		openClawThinkingStreamPatchValue1,
		openClawThinkingStreamPatchValue2,
		openClawThinkingStreamPatchValue3,
		openClawFallbackRetryPromptPatchValue,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("patched file missing %q", want)
		}
	}
}

func TestApplyOpenClawThinkingStreamHotfixFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth-profiles-test.js")
	patched := strings.Join([]string{
		openClawThinkingStreamPatchValue1,
		openClawThinkingStreamPatchValue2,
		openClawThinkingStreamPatchValue3,
		openClawFallbackRetryPromptPatchValue,
	}, "\n")
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	changed, err := applyOpenClawThinkingStreamHotfixFile(path)
	if err != nil {
		t.Fatalf("apply hotfix: %v", err)
	}
	if changed {
		t.Fatalf("expected hotfix to be idempotent")
	}
}
