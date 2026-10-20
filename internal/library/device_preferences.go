package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"attune/internal/formats"
)

func migrateDevicePreferences(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS device_preferences (
		device_id TEXT NOT NULL PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
		folders_json TEXT NOT NULL DEFAULT '[]',
		formats_json TEXT NOT NULL DEFAULT '[]',
		updated_at TEXT NOT NULL
	)`)
	return err
}

// SaveDevicePreferences stores ordered per-device folder and format overrides.
// The device must already be recorded in the library.
func (s *Store) SaveDevicePreferences(deviceID string, folders, preferredFormats []string) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("device ID is required")
	}
	cleanFolders, err := normalizeDeviceFolders(folders)
	if err != nil {
		return err
	}
	cleanFormats, err := normalizeDeviceFormats(preferredFormats)
	if err != nil {
		return err
	}
	folderJSON, err := json.Marshal(cleanFolders)
	if err != nil {
		return err
	}
	formatJSON, err := json.Marshal(cleanFormats)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO device_preferences(device_id,folders_json,formats_json,updated_at)
		VALUES(?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET folders_json=excluded.folders_json,
		formats_json=excluded.formats_json,updated_at=excluded.updated_at`,
		deviceID, string(folderJSON), string(formatJSON), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save device preferences: %w", err)
	}
	return nil
}

// LoadDevicePreferences returns saved ordered overrides and found=false when
// the device has not been customized.
func (s *Store) LoadDevicePreferences(deviceID string) (folders, preferredFormats []string, found bool, err error) {
	var folderJSON, formatJSON string
	err = s.db.QueryRow(`SELECT folders_json,formats_json FROM device_preferences WHERE device_id=?`, deviceID).Scan(&folderJSON, &formatJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if err = json.Unmarshal([]byte(folderJSON), &folders); err != nil {
		return nil, nil, false, fmt.Errorf("decode saved device folders: %w", err)
	}
	if err = json.Unmarshal([]byte(formatJSON), &preferredFormats); err != nil {
		return nil, nil, false, fmt.Errorf("decode saved device formats: %w", err)
	}
	return folders, preferredFormats, true, nil
}

// ClearDevicePreferences removes saved overrides so profile defaults apply.
func (s *Store) ClearDevicePreferences(deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("device ID is required")
	}
	_, err := s.db.Exec(`DELETE FROM device_preferences WHERE device_id=?`, deviceID)
	return err
}

func normalizeDeviceFolders(folders []string) ([]string, error) {
	clean := make([]string, 0, len(folders))
	seen := make(map[string]struct{}, len(folders))
	for _, folder := range folders {
		folder = strings.TrimSpace(strings.ReplaceAll(folder, `\`, "/"))
		if folder == "" || strings.HasPrefix(folder, "/") || path.IsAbs(folder) || (len(folder) >= 2 && folder[1] == ':') {
			return nil, fmt.Errorf("device folder must be a non-empty relative path: %q", folder)
		}
		parts := strings.Split(folder, "/")
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || strings.ContainsRune(part, '\x00') {
				return nil, fmt.Errorf("invalid component in device folder %q", folder)
			}
		}
		folder = strings.Join(parts, "/")
		key := strings.ToLower(folder)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		clean = append(clean, folder)
	}
	return clean, nil
}

func normalizeDeviceFormats(preferredFormats []string) ([]string, error) {
	clean := make([]string, 0, len(preferredFormats))
	seen := make(map[string]struct{}, len(preferredFormats))
	for _, value := range preferredFormats {
		format := formats.Normalize(value)
		if format == "" || !formats.CanTransfer(format) {
			return nil, fmt.Errorf("unsupported device transfer format %q", value)
		}
		if _, exists := seen[format]; exists {
			continue
		}
		seen[format] = struct{}{}
		clean = append(clean, format)
	}
	return clean, nil
}
