package windows

import "github.com/wailsapp/wails/v3/pkg/application"

// DefaultDefinitions 返回子窗口定义
func DefaultDefinitions() []WindowDefinition {
	return []WindowDefinition{
		{
			Name: WindowWinsnap,
			CreateOptions: func() application.WebviewWindowOptions {
				return application.WebviewWindowOptions{
					Name:   WindowWinsnap,
					Title:  "WinSnap",
					Width:  400,
					Height: 720,
					Hidden: true,
					// Use custom titlebar inside the webview.
					Frameless: true,
					// Keep the attached window above other apps on macOS too.
					AlwaysOnTop: true,
					URL:         "/winsnap.html",
					Mac: application.MacWindow{
						WindowLevel: application.MacWindowLevelFloating,
						CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
							application.MacWindowCollectionBehaviorTransient |
							application.MacWindowCollectionBehaviorIgnoresCycle,
					},
				}
			},
			// Side window should not steal focus when shown.
			FocusOnShow: false,
		},
		{
			Name: WindowTextSelection,
			CreateOptions: func() application.WebviewWindowOptions {
				return application.WebviewWindowOptions{
					Name:                       WindowTextSelection,
					Title:                      "TextSelection",
					Width:                      140,
					Height:                     50,
					Hidden:                     true,
					Frameless:                  true,
					AlwaysOnTop:                true,
					DisableResize:              true,
					BackgroundType:             application.BackgroundTypeTransparent,
					DefaultContextMenuDisabled: true,
					InitialPosition:            application.WindowXY,
					URL:                        "/selection.html",
					// Windows specific: hide from taskbar
					Windows: application.WindowsWindow{
						HiddenOnTaskbar: true,
					},
					Mac: application.MacWindow{
						Backdrop:    application.MacBackdropTransparent,
						WindowLevel: application.MacWindowLevelFloating,
						CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
							application.MacWindowCollectionBehaviorTransient |
							application.MacWindowCollectionBehaviorIgnoresCycle,
					},
				}
			},
			// Popup should not steal focus when shown.
			FocusOnShow: false,
		},
	}
}
