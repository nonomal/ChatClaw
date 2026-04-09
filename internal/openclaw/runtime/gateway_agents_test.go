package openclawruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAgentWorkspaceStateDir(t *testing.T) {
	workspaceDir := filepath.Join(t.TempDir(), "workspace-agent-main")
	if err := ensureAgentWorkspaceStateDir(workspaceDir); err != nil {
		t.Fatalf("ensureAgentWorkspaceStateDir() error = %v", err)
	}

	if info, err := os.Stat(workspaceDir); err != nil {
		t.Fatalf("stat workspace dir: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("workspace dir %q is not a directory", workspaceDir)
	}

	stateDir := filepath.Join(workspaceDir, ".openclaw")
	if info, err := os.Stat(stateDir); err != nil {
		t.Fatalf("stat state dir: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("state dir %q is not a directory", stateDir)
	}
}

func TestIsWorkspaceStateRenameENOENT(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "matches workspace state rename enoent",
			err:  os.ErrNotExist,
			want: false,
		},
		{
			name: "matches gateway rename error",
			err:  errString("ENOENT: rename workspace-state.json.tmp -> workspace-state.json"),
			want: true,
		},
		{
			name: "rejects unrelated enoent",
			err:  errString("ENOENT: open some-other-file"),
			want: false,
		},
		{
			name: "rejects non rename workspace state error",
			err:  errString("workspace-state.json missing"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWorkspaceStateRenameENOENT(tt.err); got != tt.want {
				t.Fatalf("isWorkspaceStateRenameENOENT(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsAgentAlreadyExistsError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "matches agent already exists",
			err:  errString("agent codex/main already exists"),
			want: true,
		},
		{
			name: "rejects non agent already exists",
			err:  errString("workspace already exists"),
			want: false,
		},
		{
			name: "rejects nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAgentAlreadyExistsError(tt.err); got != tt.want {
				t.Fatalf("isAgentAlreadyExistsError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}
