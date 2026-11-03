package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// The window/taskbar icon. Wails packages the same PNG as the macOS .icns and
// build/windows/icon.ico is generated from it by tools/icon.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Drill",
		Width:            1200,
		Height:           820,
		MinWidth:         880,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 22, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		Bind:             []interface{}{app},
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "drill",
			// OnDemand avoids the WebKitGTK crashes people hit with always-on
			// acceleration, without forcing software rendering everywhere.
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
