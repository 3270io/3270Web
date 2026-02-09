//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func runAppWindow(url string, onExit func()) {
	var once sync.Once
	var mw *walk.MainWindow
	var appIcon *walk.Icon

	bgBrush := SolidColorBrush{Color: walk.RGB(10, 14, 10)}
	panelBrush := SolidColorBrush{Color: walk.RGB(15, 20, 15)}
	borderBrush := SolidColorBrush{Color: walk.RGB(30, 43, 30)}
	fgColor := walk.RGB(51, 204, 51)

	if icon, err := loadAppIconFromResource(); err == nil {
		appIcon = icon
	} else if icon, err := loadAppIconFromFile(); err == nil {
		appIcon = icon
	}

	onClose := func() {
		once.Do(func() {
			if onExit != nil {
				onExit()
			}
		})
	}
	versionText := appVersion
	if versionText == "" {
		versionText = "dev"
	}
	windowTitle := "3270Web"

	mainWindow := MainWindow{
		AssignTo:   &mw,
		Title:      windowTitle,
		MinSize:    Size{Width: 520, Height: 280},
		Size:       Size{Width: 620, Height: 320},
		Background: bgBrush,
		Layout:     VBox{MarginsZero: false, Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18}, Spacing: 12},
		Children: []Widget{
			Label{
				Text:      "3270Web",
				TextColor: fgColor,
				Font:      Font{Family: "Consolas", PointSize: 18, Bold: true},
			},
			Label{
				Text:      "3270Web is running.",
				TextColor: fgColor,
				Font:      Font{Family: "Consolas", PointSize: 11},
			},
			Label{
				Text:      "Use the button below to open the UI in your browser.",
				TextColor: fgColor,
				Font:      Font{Family: "Consolas", PointSize: 10},
			},
			Label{
				Text:      "v" + versionText,
				TextColor: fgColor,
				Font:      Font{Family: "Consolas", PointSize: 10},
			},
			Composite{
				Background: panelBrush,
				Layout:     HBox{Spacing: 10, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
				Children: []Widget{
					PushButton{
						Text:       "Open 3270Web",
						Background: borderBrush,
						Font:       Font{Family: "Consolas", PointSize: 10, Bold: true},
						OnClicked: func() {
							openBrowser(url)
						},
					},
					PushButton{
						Text:       "Exit",
						Background: borderBrush,
						Font:       Font{Family: "Consolas", PointSize: 10},
						OnClicked: func() {
							onClose()
							mw.Close()
						},
					},
				},
			},
		},
	}
	if appIcon != nil {
		mainWindow.Icon = appIcon
	}
	if err := mainWindow.Create(); err != nil {
		showFatalError(fmt.Sprintf("Failed to create app window. %v", err))
		return
	}
	if mw == nil {
		showFatalError("Failed to create app window.")
		return
	}
	if appIcon != nil {
		_ = mw.SetIcon(appIcon)
	}

	mw.Show()
	mw.Run()
}

func loadAppIconFromFile() (*walk.Icon, error) {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "3270Web_logo.png"),
			filepath.Join(exeDir, "static", "3270Web_logo.png"),
			filepath.Join(exeDir, "web", "static", "3270Web_logo.png"),
			filepath.Join(exeDir, "..", "web", "static", "3270Web_logo.png"),
		}
		for _, candidate := range candidates {
			if _, statErr := os.Stat(candidate); statErr == nil {
				return walk.NewIconFromFile(candidate)
			}
		}
	}

	return walk.NewIconFromFile(filepath.FromSlash("web/static/3270Web_logo.png"))
}

func loadAppIconFromResource() (*walk.Icon, error) {
	if icon, err := walk.NewIconFromResource("APPICON"); err == nil {
		return icon, nil
	}
	return walk.NewIconFromResourceId(1)
}
