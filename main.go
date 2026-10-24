package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"

	"attune/internal/updater"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--attune-update-helper" {
		if err := updater.RunHelper(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	app := NewApp()
	err := wails.Run(&options.App{
		Title: "Attune", Width: 1240, Height: 820, MinWidth: 900, MinHeight: 600,
		BackgroundColour: options.NewRGBA(0, 0, 0, 0),
		Frameless:        runtime.GOOS == "linux",
		Linux:            &linux.Options{WindowIsTranslucent: runtime.GOOS == "linux"},
		AssetServer: &assetserver.Options{Assets: assets, Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/book-archives/") {
					app.bookArchive(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/book-files/") {
					app.bookFile(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/preview/") {
					app.previewResource(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/covers/") {
					app.covers(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}},
		OnStartup: app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose,
		Bind:               []interface{}{app},
		DragAndDrop:        &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "ba8d8086-f57b-4fbe-8260-2d3ee1c5e015"},
		Mac:                &mac.Options{TitleBar: &mac.TitleBar{TitlebarAppearsTransparent: true, HideTitle: true, FullSizeContent: true, UseToolbar: false, HideToolbarSeparator: true}, WebviewIsTransparent: true, WindowIsTranslucent: true, About: &mac.AboutInfo{Title: "Attune", Message: "Attune your library."}},
	})
	if err != nil {
		log.Fatal(err)
	}
}
