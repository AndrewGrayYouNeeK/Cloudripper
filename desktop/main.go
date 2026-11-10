//go:build desktop

package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	notes := notifications.New()
	service := NewAppService(notes)
	app := application.New(application.Options{
		Name:        "CloudRipper",
		Description: "Read-only desktop app for cloud inventory, spend, recommendations, bills, and cost alerts",
		Services: []application.Service{
			application.NewService(notes),
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	service.Attach(app)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "CloudRipper",
		Width:            1120,
		Height:           760,
		MinWidth:         720,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(16, 21, 29),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
