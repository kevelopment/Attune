//go:build darwin || linux

package device

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// unixMTPBackend is the small boundary between the contract adapter and the
// optional libmtp binding. IDs returned by it remain opaque to callers.
type unixMTPBackend interface {
	Discover(context.Context) ([]TransportDevice, error)
	Open(context.Context, string) (unixMTPConnection, error)
}

type unixMTPConnection interface {
	Storages(context.Context) ([]TransportStorage, error)
	List(context.Context, string, string) ([]RemoteObject, error)
	Download(context.Context, string, string, io.Writer, ProgressFunc) error
	Upload(context.Context, string, string, string, io.Reader, int64, ProgressFunc) (RemoteObject, error)
	Delete(context.Context, string, string) error
	Disconnect(context.Context) error
}

// NewUnixMTPTransport returns the Unix MTP adapter. The default build keeps
// working without system libmtp; see docs/mtp-unix.md for the optional binding.
func NewUnixMTPTransport() DeviceTransport {
	return unixMTPTransport{backend: newUnixMTPBackend()}
}

func (unixMTPTransport) ID() string { return "mtp-libmtp" }

type unixMTPTransport struct{ backend unixMTPBackend }

func (t unixMTPTransport) Discover(ctx context.Context) ([]TransportDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	devices, err := t.backend.Discover(ctx)
	return devices, translateMTPError(err)
}

func (t unixMTPTransport) Open(ctx context.Context, id string) (DeviceSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("MTP device ID is required")
	}
	conn, err := t.backend.Open(ctx, id)
	if err != nil {
		return nil, translateMTPError(err)
	}
	return &unixMTPSession{conn: conn}, nil
}

type unixMTPSession struct {
	mu     sync.Mutex
	conn   unixMTPConnection
	closed bool
}

func (s *unixMTPSession) with(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("MTP session is disconnected")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return translateMTPError(fn())
}

func (s *unixMTPSession) Storages(ctx context.Context) (out []TransportStorage, err error) {
	err = s.with(ctx, func() error { var e error; out, e = s.conn.Storages(ctx); return e })
	return
}
func (s *unixMTPSession) List(ctx context.Context, storageID, parentID string) (out []RemoteObject, err error) {
	if storageID == "" {
		return nil, errors.New("MTP storage ID is required")
	}
	err = s.with(ctx, func() error { var e error; out, e = s.conn.List(ctx, storageID, parentID); return e })
	return
}
func (s *unixMTPSession) Download(ctx context.Context, storageID, objectID string, dst io.Writer, progress ProgressFunc) error {
	if storageID == "" || objectID == "" || dst == nil {
		return errors.New("MTP storage, object, and destination are required")
	}
	return s.with(ctx, func() error { return s.conn.Download(ctx, storageID, objectID, dst, contextProgress(ctx, progress)) })
}
func (s *unixMTPSession) Upload(ctx context.Context, storageID, parentID, name string, src io.Reader, size int64, progress ProgressFunc) (out RemoteObject, err error) {
	if storageID == "" || name == "" || strings.ContainsAny(name, "/\\\x00") || src == nil || size < 0 {
		return RemoteObject{}, errors.New("valid MTP storage, filename, reader, and size are required")
	}
	err = s.with(ctx, func() error {
		children, e := s.conn.List(ctx, storageID, parentID)
		if e != nil {
			return e
		}
		for _, child := range children {
			if !child.Directory && child.Name == name {
				return fmt.Errorf("refusing to overwrite existing MTP object %q: %w", name, ErrObjectExists)
			}
		}
		created, uploadErr := s.conn.Upload(ctx, storageID, parentID, name, src, size, contextProgress(ctx, progress))
		if uploadErr != nil {
			if created.ID != "" {
				_ = s.cleanupCreated(storageID, created.ID, ctx)
			}
			return uploadErr
		}
		out = created
		if out.ID != "" {
			current, listErr := s.conn.List(ctx, storageID, parentID)
			if listErr != nil {
				_ = s.cleanupCreated(storageID, out.ID, ctx)
				out = RemoteObject{}
				return fmt.Errorf("verify MTP upload directory: %w", listErr)
			}
			for _, child := range current {
				if !child.Directory && child.Name == name && child.ID != out.ID {
					_ = s.cleanupCreated(storageID, out.ID, ctx)
					out = RemoteObject{}
					return fmt.Errorf("refusing to overwrite existing MTP object %q: %w", name, ErrObjectExists)
				}
			}
		}
		if out.ID == "" || out.Directory || out.Name != name || out.Size != size {
			if out.ID != "" {
				_ = s.cleanupCreated(storageID, out.ID, ctx)
			}
			out = RemoteObject{}
			return fmt.Errorf("MTP upload verification failed: expected %d bytes named %q", size, name)
		}
		return nil
	})
	return out, err
}
func (s *unixMTPSession) cleanupCreated(storageID, objectID string, ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.conn.Delete(cleanupCtx, storageID, objectID)
}

func (s *unixMTPSession) Delete(ctx context.Context, storageID, objectID string) error {
	if storageID == "" || objectID == "" {
		return errors.New("MTP storage and object IDs are required")
	}
	return s.with(ctx, func() error { return s.conn.Delete(ctx, storageID, objectID) })
}
func (s *unixMTPSession) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := ctx.Err(); err != nil {
		return err
	}
	return translateMTPError(s.conn.Disconnect(ctx))
}

func contextProgress(ctx context.Context, progress ProgressFunc) ProgressFunc {
	if progress == nil {
		return nil
	}
	return func(done, total uint64) {
		if ctx.Err() == nil {
			progress(done, total)
		}
	}
}

func translateMTPError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "busy"), strings.Contains(msg, "locked"), strings.Contains(msg, "claim interface"):
		return fmt.Errorf("MTP device is busy or locked by another app; close other device managers and retry: %w", err)
	case strings.Contains(msg, "disconnect"), strings.Contains(msg, "no device"), strings.Contains(msg, "not found"):
		return fmt.Errorf("MTP device was disconnected; reconnect it and retry: %w", err)
	default:
		return err
	}
}
