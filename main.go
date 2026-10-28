package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"

	appcore "attune/internal/app"
	"attune/internal/model"
	"attune/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[model.ImportProgress]("import:progress")
	application.RegisterEvent[model.Progress]("transfer:progress")
	application.RegisterEvent[appcore.UpdateState]("update:state")
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--attune-update-helper" {
		if err := updater.RunHelper(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "--attune-update-helper-root" {
		if err := updater.RunElevatedHelper(os.Args[2], os.Args[3]); err != nil {
			log.Fatal(err)
		}
		return
	}
	service := NewApp()
	app := application.New(application.Options{
		Name: "Attune", Description: "Attune your library.",
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets), Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/book-archives/") {
					service.bookArchive(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/book-files/") {
					service.bookFile(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/preview/") {
					service.previewResource(w, r)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/covers/") {
					service.covers(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}},
		Services:       []application.Service{application.NewService(service)},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "ba8d8086-f57b-4fbe-8260-2d3ee1c5e015"},
		ShouldQuit:     func() bool { return !service.core.BeforeClose() },
		Mac:            application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Attune", Width: 1240, Height: 820, MinWidth: 900, MinHeight: 600,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Frameless:        runtime.GOOS == "linux",
		EnableFileDrop:   true,
		Linux:            application.LinuxWindow{WindowIsTranslucent: runtime.GOOS == "linux"},
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBar{AppearsTransparent: true, HideTitle: true, FullSizeContent: true, UseToolbar: false, HideToolbarSeparator: true},
		},
	})
	service.core.SetDesktop(newWailsDesktop(app))
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		details := event.Context().DropTargetDetails()
		if details == nil {
			return
		}
		app.Event.Emit("files:dropped", struct {
			X     int      `json:"x"`
			Y     int      `json:"y"`
			Files []string `json:"files"`
		}{X: details.X, Y: details.Y, Files: event.Context().DroppedFiles()})
	})
	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func newWailsDesktop(app *application.App) appcore.Desktop {
	return appcore.Desktop{
		Emit: func(event string, data ...any) { app.Event.Emit(event, data...) },
		OpenFiles: func(title, filterName, pattern string) ([]string, error) {
			return app.Dialog.OpenFile().SetTitle(title).AddFilter(filterName, pattern).PromptForMultipleSelection()
		},
		OpenFile: func(title, filterName, pattern string) (string, error) {
			return app.Dialog.OpenFile().SetTitle(title).AddFilter(filterName, pattern).PromptForSingleSelection()
		},
		OpenDirectory: func(title, initialDirectory string) (string, error) {
			dialog := app.Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).SetTitle(title)
			if initialDirectory != "" {
				dialog.SetDirectory(initialDirectory)
			}
			return dialog.PromptForSingleSelection()
		},
		Confirm: func(title, message string, buttons []string, defaultButton, cancelButton string) (string, error) {
			selected := ""
			dialog := app.Dialog.Question().SetTitle(title).SetMessage(message)
			var defaultChoice, cancelChoice *application.Button
			for _, label := range buttons {
				button := dialog.AddButton(label).OnClick(func() { selected = label })
				if label == defaultButton {
					defaultChoice = button
				}
				if label == cancelButton {
					cancelChoice = button
				}
			}
			if defaultChoice != nil {
				dialog.SetDefaultButton(defaultChoice)
			}
			if cancelChoice != nil {
				dialog.SetCancelButton(cancelChoice)
			}
			dialog.Show()
			return selected, nil
		},
		Quit: app.Quit,
	}
}
