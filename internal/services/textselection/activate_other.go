//go:build !windows

package textselection

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// forceActivateWindow on non-Windows platforms directly calls Focus.
func forceActivateWindow(w *application.WebviewWindow) {
	if w != nil {
		w.Focus()
	}
}
