package main

import (
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Rancher HA RKE2",
		Width:     1440,
		Height:    1000,
		MinWidth:  980,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Handler: app,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 18, B: 24, A: 1},
		Mac: &mac.Options{
			Preferences: &mac.Preferences{
				FullscreenEnabled: mac.Enabled,
				TabFocusesLinks:   mac.Enabled,
			},
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
