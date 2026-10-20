package device

import (
	"context"
	"errors"
	"io"
)

var ErrObjectExists = errors.New("device object already exists")

// DeviceTransport discovers a transport's devices and opens one serialized
// session at a time. Profiles remain independent of this connection layer.
type DeviceTransport interface {
	ID() string
	Discover(context.Context) ([]TransportDevice, error)
	Open(context.Context, string) (DeviceSession, error)
}

// DeviceSession exposes storage and file operations without treating a remote
// object identifier as a filesystem path. Calls on a session are serialized.
type DeviceSession interface {
	Storages(context.Context) ([]TransportStorage, error)
	// List returns files beneath one configured folder only. Implementations may
	// enumerate that subtree recursively, but must never crawl unrelated storage.
	List(context.Context, string, string) ([]RemoteObject, error)
	Download(context.Context, string, string, io.Writer, ProgressFunc) error
	Upload(context.Context, string, string, string, io.Reader, int64, ProgressFunc) (RemoteObject, error)
	Delete(context.Context, string, string) error
	Disconnect(context.Context) error
}

type TransportDevice struct {
	ID      string
	Name    string
	Vendor  string
	Product string
}

type TransportStorage struct {
	ID         string
	Name       string
	FreeBytes  uint64
	TotalBytes uint64
}

type RemoteObject struct {
	ID           string
	Name         string
	RelativePath string
	Size         int64
	Directory    bool
}

type ProgressFunc func(done, total uint64)
