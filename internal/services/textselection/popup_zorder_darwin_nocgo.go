//go:build darwin && !cgo

package textselection

import "github.com/wailsapp/wails/v3/pkg/application"

// hidePopupNative hides the popup window using the platform's native hide mechanism.
func hidePopupNative(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	w.Hide()
}

// forcePopupTopMostNoActivate is a no-op without CGO on macOS.
func forcePopupTopMostNoActivate(_ *application.WebviewWindow) {}
