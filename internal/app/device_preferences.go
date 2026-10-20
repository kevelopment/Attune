package app

import (
	"errors"
)

// DevicePreferences exposes the effective transfer defaults plus whether the
// user has saved an override for this physical device.
type DevicePreferences struct {
	Folders    []string `json:"folders"`
	Formats    []string `json:"formats"`
	Customized bool     `json:"customized"`
}

func (a *App) GetDevicePreferences(deviceID string) (DevicePreferences, error) {
	d, err := a.findDevice(deviceID)
	if err != nil {
		return DevicePreferences{}, err
	}
	folders, formats, found, err := a.store.LoadDevicePreferences(d.ID)
	if err != nil {
		return DevicePreferences{}, err
	}
	if !found {
		folders = append([]string(nil), d.BookFolders...)
		formats = append([]string(nil), d.TransferFormats...)
	}
	if folders == nil {
		folders = []string{}
	}
	if formats == nil {
		formats = []string{}
	}
	return DevicePreferences{Folders: folders, Formats: formats, Customized: found}, nil
}

func (a *App) SaveDevicePreferences(deviceID string, folders, formats []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	d, err := a.findDevice(deviceID)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return errors.New("add at least one relative book folder")
	}
	if len(formats) == 0 {
		return errors.New("choose at least one format this reader can open")
	}
	if err = a.store.SaveDevicePreferences(d.ID, folders, formats); err != nil {
		return err
	}
	a.event("devices:changed")
	return nil
}

func (a *App) ResetDevicePreferences(deviceID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	d, err := a.findDevice(deviceID)
	if err != nil {
		return err
	}
	if err = a.store.ClearDevicePreferences(d.ID); err != nil {
		return err
	}
	a.event("devices:changed")
	return nil
}
