package app

import "errors"

// Desktop provides the small set of native Wails operations used by the app
// core. Keeping these callbacks outside the core makes the library and updater
// code independent from Wails' runtime API.
type Desktop struct {
	Emit          func(string, ...any)
	OpenFiles     func(title, filterName, pattern string) ([]string, error)
	OpenFile      func(title, filterName, pattern string) (string, error)
	OpenDirectory func(title, initialDirectory string) (string, error)
	Confirm       func(title, message string, buttons []string, defaultButton, cancelButton string) (string, error)
	Quit          func()
}

func (a *App) SetDesktop(desktop Desktop) {
	a.desktop = desktop
	if desktop.Emit != nil {
		a.emit = desktop.Emit
	}
}

func (a *App) openFiles(title, filterName, pattern string) ([]string, error) {
	if a.desktop.OpenFiles == nil {
		return nil, errors.New("native file picker is unavailable")
	}
	return a.desktop.OpenFiles(title, filterName, pattern)
}

func (a *App) openFile(title, filterName, pattern string) (string, error) {
	if a.desktop.OpenFile == nil {
		return "", errors.New("native file picker is unavailable")
	}
	return a.desktop.OpenFile(title, filterName, pattern)
}

func (a *App) openDirectory(title, initialDirectory string) (string, error) {
	if a.desktop.OpenDirectory == nil {
		return "", errors.New("native folder picker is unavailable")
	}
	return a.desktop.OpenDirectory(title, initialDirectory)
}
