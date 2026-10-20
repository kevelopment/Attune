package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"attune/internal/formats"
	"attune/internal/model"
)

var errRemoteFolderNotFound = errors.New("configured reader folder was not found")

func (m *Manager) discoverTransportDevices(ctx context.Context) ([]model.Device, map[string]bool, []error) {
	devices := make([]model.Device, 0)
	failed := make(map[string]bool)
	var errs []error
	for _, transport := range m.transports {
		if err := ctx.Err(); err != nil {
			return devices, failed, append(errs, err)
		}
		found, err := transport.Discover(ctx)
		if err != nil {
			failed[transport.ID()] = true
			errs = append(errs, fmt.Errorf("discover %s: %w", transport.ID(), err))
			continue
		}
		transportDevices := make([]model.Device, 0)
		transportFailed := false
		for _, foundDevice := range found {
			if err := ctx.Err(); err != nil {
				return devices, failed, append(errs, err)
			}
			session, openErr := transport.Open(ctx, foundDevice.ID)
			if openErr != nil {
				transportFailed = true
				errs = append(errs, fmt.Errorf("open %s: %w", foundDevice.Name, openErr))
				continue
			}
			storages, storageErr := session.Storages(ctx)
			_ = session.Disconnect(context.WithoutCancel(ctx))
			if storageErr != nil {
				transportFailed = true
				errs = append(errs, fmt.Errorf("list storage for %s: %w", foundDevice.Name, storageErr))
				continue
			}
			for _, storage := range storages {
				profile := ResolveProfile(DeviceDescriptor{Label: foundDevice.Name, Name: storage.Name, Vendor: foundDevice.Vendor, Product: foundDevice.Product})
				name := foundDevice.Name
				if name == "" {
					name = profile.Name
				}
				if storage.Name != "" {
					name += " · " + storage.Name
				}
				transportDevices = append(transportDevices, model.Device{
					ID: transportStorageID(transport.ID(), foundDevice.ID, storage.ID), Name: name,
					Transport: transport.ID(), TransportDeviceID: foundDevice.ID, StorageID: storage.ID,
					FreeBytes: storage.FreeBytes, TotalBytes: storage.TotalBytes, ProfileID: profile.ID,
					BookFolders:               append([]string(nil), profile.BookFolders...),
					TransferFormats:           append([]string(nil), profile.TransferFormats...),
					AllowEPUBToMOBI:           profile.AllowEPUBToMOBI,
					RequiresExplicitSelection: profile.RequiresExplicitSelection,
				})
			}
		}
		if transportFailed {
			failed[transport.ID()] = true
			continue
		}
		devices = append(devices, transportDevices...)
	}
	return devices, failed, errs
}

func transportStorageID(transportID, deviceID, storageID string) string {
	key := sha256.Sum256([]byte(transportID + "\x00" + deviceID + "\x00" + storageID))
	return "mtp:" + transportID + ":" + hex.EncodeToString(key[:16])
}

func (m *Manager) openTransportSession(ctx context.Context, expected model.Device) (DeviceTransport, DeviceSession, error) {
	m.init()
	transport := m.transports[expected.Transport]
	if transport == nil {
		return nil, nil, fmt.Errorf("transport %q is unavailable; reconnect the reader and try again", expected.Transport)
	}
	if expected.TransportDeviceID == "" || expected.StorageID == "" || expected.ID != transportStorageID(expected.Transport, expected.TransportDeviceID, expected.StorageID) {
		return nil, nil, errors.New("reader identity is incomplete or changed; reconnect it and try again")
	}
	devices, err := transport.Discover(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("discover reader: %w", err)
	}
	connected := false
	for _, candidate := range devices {
		if candidate.ID == expected.TransportDeviceID {
			connected = true
			break
		}
	}
	if !connected {
		return nil, nil, errors.New("reader disconnected; reconnect it and try again")
	}
	session, err := transport.Open(ctx, expected.TransportDeviceID)
	if err != nil {
		return nil, nil, fmt.Errorf("open reader session: %w", err)
	}
	storages, err := session.Storages(ctx)
	if err != nil {
		_ = session.Disconnect(context.Background())
		return nil, nil, fmt.Errorf("list reader storage: %w", err)
	}
	for _, storage := range storages {
		if storage.ID == expected.StorageID {
			return transport, session, nil
		}
	}
	_ = session.Disconnect(context.Background())
	return nil, nil, errors.New("reader storage changed; reconnect the reader and try again")
}

func (m *Manager) listTransportBooks(expected model.Device) ([]model.DeviceBook, error) {
	ctx := context.Background()
	_, session, err := m.openTransportSession(ctx, expected)
	if err != nil {
		return nil, err
	}
	defer session.Disconnect(context.Background())
	books := make([]model.DeviceBook, 0)
	seen := make(map[string]struct{})
	for _, folder := range uniqueFolders(expected.BookFolders) {
		objects, listErr := listTransportFolder(ctx, session, expected.StorageID, folder)
		if listErr != nil {
			if errors.Is(listErr, errRemoteFolderNotFound) {
				continue
			}
			return nil, fmt.Errorf("list %s on %s: %w", folder, expected.Name, listErr)
		}
		for _, object := range objects {
			if object.Directory || object.ID == "" || !supportedDeviceFormat(object.Name, expected.TransferFormats) {
				continue
			}
			relative := object.RelativePath
			clean, pathErr := validateRelativeDevicePath(relative)
			if pathErr != nil || !containsBookFolderPath(expected.BookFolders, clean) {
				continue
			}
			if _, exists := seen[object.ID]; exists {
				continue
			}
			seen[object.ID] = struct{}{}
			books = append(books, model.DeviceBook{
				Path: object.ID, Name: object.Name, Size: object.Size,
				CanRead: formats.CanRead(filepath.Ext(object.Name)),
				Title:   strings.TrimSuffix(object.Name, filepath.Ext(object.Name)),
			})
		}
	}
	return books, nil
}

func (m *Manager) sendTransport(ctx context.Context, expected model.Device, source, filename string, progress ProgressFunc) (string, error) {
	if !supportedDeviceFormat(filename, expected.TransferFormats) || filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return "", fmt.Errorf("filename must use a supported device ebook format (%s)", strings.Join(expected.TransferFormats, ", "))
	}
	if len(expected.BookFolders) == 0 {
		return "", errors.New("choose a book folder in reader transfer settings before sending")
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("source must be a regular file")
	}
	if uint64(info.Size()) > expected.FreeBytes && expected.FreeBytes > 0 {
		return "", fmt.Errorf("not enough free space: need %d bytes, have %d", info.Size(), expected.FreeBytes)
	}
	_, session, err := m.openTransportSession(ctx, expected)
	if err != nil {
		return "", err
	}
	defer session.Disconnect(context.Background())
	parentID := ""
	var folderErr error
	for _, folder := range uniqueFolders(expected.BookFolders) {
		parentID, folderErr = resolveTransportFolder(ctx, session, expected.StorageID, folder)
		if folderErr == nil {
			break
		}
		if !errors.Is(folderErr, errRemoteFolderNotFound) {
			return "", fmt.Errorf("open book folder %s: %w", folder, folderErr)
		}
	}
	if folderErr != nil {
		return "", errors.New("none of the configured reader book folders exists; choose a valid folder in reader transfer settings")
	}
	object, err := session.Upload(ctx, expected.StorageID, parentID, filename, input, info.Size(), progress)
	if err != nil {
		if errors.Is(err, ErrObjectExists) {
			return "", fmt.Errorf("refusing to overwrite existing device file %q", filename)
		}
		return "", err
	}
	if object.ID == "" || object.Directory {
		if object.ID != "" {
			_ = session.Delete(context.Background(), expected.StorageID, object.ID)
		}
		return "", errors.New("device did not confirm the uploaded book")
	}
	if object.Size != info.Size() {
		_ = session.Delete(context.Background(), expected.StorageID, object.ID)
		return "", fmt.Errorf("device upload size mismatch: sent %d bytes, device reports %d", info.Size(), object.Size)
	}
	return object.ID, nil
}

func (m *Manager) removeTransportBook(expected model.Device, objectID string) error {
	if objectID == "" {
		return errors.New("device object identity is required")
	}
	ctx := context.Background()
	_, session, err := m.openTransportSession(ctx, expected)
	if err != nil {
		return err
	}
	defer session.Disconnect(context.Background())
	for _, folder := range uniqueFolders(expected.BookFolders) {
		objects, listErr := listTransportFolder(ctx, session, expected.StorageID, folder)
		if listErr != nil {
			if errors.Is(listErr, errRemoteFolderNotFound) {
				continue
			}
			return fmt.Errorf("list %s on %s: %w", folder, expected.Name, listErr)
		}
		for _, object := range objects {
			if object.ID != objectID {
				continue
			}
			if object.Directory || !supportedDeviceFormat(object.Name, expected.TransferFormats) || !containsBookFolderPath(expected.BookFolders, object.RelativePath) {
				return errors.New("removal is limited to supported ebook files inside configured reader folders")
			}
			return session.Delete(ctx, expected.StorageID, object.ID)
		}
	}
	return errors.New("device book is no longer available")
}

func resolveTransportFolder(ctx context.Context, session DeviceSession, storageID, folder string) (string, error) {
	clean, err := validateRelativeDevicePath(folder)
	if err != nil {
		return "", err
	}
	parentID := ""
	for _, component := range strings.Split(filepath.ToSlash(clean), "/") {
		children, err := session.List(ctx, storageID, parentID)
		if err != nil {
			return "", err
		}
		found := false
		for _, child := range children {
			if child.Directory && strings.EqualFold(child.Name, component) && child.ID != "" {
				parentID = child.ID
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("%w: %s", errRemoteFolderNotFound, clean)
		}
	}
	return parentID, nil
}

func listTransportFolder(ctx context.Context, session DeviceSession, storageID, folder string) ([]RemoteObject, error) {
	parentID, err := resolveTransportFolder(ctx, session, storageID, folder)
	if err != nil {
		return nil, err
	}
	var objects []RemoteObject
	visited := make(map[string]struct{})
	var walk func(string, string, int) error
	walk = func(parent, prefix string, depth int) error {
		if depth > 32 {
			return errors.New("reader folder nesting exceeds the safe limit")
		}
		children, err := session.List(ctx, storageID, parent)
		if err != nil {
			return err
		}
		for _, child := range children {
			if child.ID == "" || child.Name == "" || child.Name == "." || child.Name == ".." || strings.ContainsAny(child.Name, "/\\\x00") {
				continue
			}
			child.RelativePath = pathpkg.Join(prefix, child.Name)
			objects = append(objects, child)
			if !child.Directory {
				continue
			}
			if _, ok := visited[child.ID]; ok {
				continue
			}
			visited[child.ID] = struct{}{}
			if err := walk(child.ID, child.RelativePath, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(parentID, filepath.ToSlash(folder), 0); err != nil {
		return nil, err
	}
	return objects, nil
}

func uniqueFolders(folders []string) []string {
	seen := make(map[string]struct{}, len(folders))
	result := make([]string, 0, len(folders))
	for _, folder := range folders {
		clean, err := validateRelativeDevicePath(folder)
		if err != nil {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, clean)
	}
	return result
}
