//go:build darwin || linux

package device

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeUnixBackend struct{ conn *fakeUnixConn }

func (b fakeUnixBackend) Discover(context.Context) ([]TransportDevice, error) {
	return []TransportDevice{{ID: "opaque-device"}}, nil
}
func (b fakeUnixBackend) Open(context.Context, string) (unixMTPConnection, error) { return b.conn, nil }

type fakeUnixConn struct {
	mu           sync.Mutex
	objects      []RemoteObject
	uploadResult RemoteObject
	uploadErr    error
	deleted      []string
	active       atomic.Int32
	maxActive    atomic.Int32
	gate         time.Duration
	afterUpload  func(RemoteObject)
}

func (f *fakeUnixConn) Storages(context.Context) ([]TransportStorage, error) {
	f.enter()
	defer f.leave()
	return []TransportStorage{{ID: "s"}}, nil
}
func (f *fakeUnixConn) List(context.Context, string, string) ([]RemoteObject, error) {
	f.enter()
	defer f.leave()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RemoteObject(nil), f.objects...), nil
}
func (f *fakeUnixConn) Download(_ context.Context, _, _ string, w io.Writer, p ProgressFunc) error {
	f.enter()
	defer f.leave()
	_, e := w.Write([]byte("ok"))
	if p != nil {
		p(2, 2)
	}
	return e
}
func (f *fakeUnixConn) Upload(_ context.Context, _, _, name string, r io.Reader, size int64, p ProgressFunc) (RemoteObject, error) {
	f.enter()
	defer f.leave()
	data, e := io.ReadAll(r)
	if e != nil {
		return RemoteObject{}, e
	}
	if p != nil {
		p(uint64(len(data)), uint64(size))
	}
	result := f.uploadResult
	if result.Name == "" {
		result = RemoteObject{ID: "new-id", Name: name, Size: int64(len(data))}
	}
	if f.afterUpload != nil {
		f.afterUpload(result)
	}
	return result, f.uploadErr
}
func (f *fakeUnixConn) Delete(_ context.Context, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeUnixConn) Disconnect(context.Context) error { return nil }
func (f *fakeUnixConn) enter() {
	n := f.active.Add(1)
	for {
		old := f.maxActive.Load()
		if n <= old || f.maxActive.CompareAndSwap(old, n) {
			break
		}
	}
	if f.gate > 0 {
		time.Sleep(f.gate)
	}
}
func (f *fakeUnixConn) leave() { f.active.Add(-1) }

func TestUnixMTPUploadNoOverwriteAndCleanup(t *testing.T) {
	conn := &fakeUnixConn{objects: []RemoteObject{{ID: "old", Name: "book.epub", Size: 2}}}
	s := &unixMTPSession{conn: conn}
	if _, err := s.Upload(context.Background(), "s", "", "book.epub", bytes.NewReader([]byte("ok")), 2, nil); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("expected no-overwrite error, got %v", err)
	}
	if len(conn.deleted) != 0 {
		t.Fatalf("existing object was deleted: %v", conn.deleted)
	}
	conn.objects = nil
	conn.uploadErr = errors.New("USB transfer interrupted")
	conn.uploadResult = RemoteObject{ID: "created-by-this-upload", Name: "book.epub", Size: 2}
	if _, err := s.Upload(context.Background(), "s", "", "book.epub", bytes.NewReader([]byte("ok")), 2, nil); err == nil {
		t.Fatal("expected upload error")
	}
	if len(conn.deleted) != 1 || conn.deleted[0] != "created-by-this-upload" {
		t.Fatalf("cleanup deleted wrong objects: %v", conn.deleted)
	}
}
func TestUnixMTPUploadDetectsConcurrentNameAndCleansOnlyItsObject(t *testing.T) {
	conn := &fakeUnixConn{}
	conn.afterUpload = func(RemoteObject) {
		conn.mu.Lock()
		conn.objects = append(conn.objects, RemoteObject{ID: "other", Name: "book.epub", Size: 3})
		conn.mu.Unlock()
	}
	s := &unixMTPSession{conn: conn}
	if _, err := s.Upload(context.Background(), "s", "", "book.epub", bytes.NewReader([]byte("ok")), 2, nil); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("expected concurrent no-overwrite error, got %v", err)
	}
	if len(conn.deleted) != 1 || conn.deleted[0] != "new-id" {
		t.Fatalf("cleanup touched wrong object: %v", conn.deleted)
	}
}

func TestUnixMTPUploadSizeMismatchCleansCreatedObject(t *testing.T) {
	conn := &fakeUnixConn{uploadResult: RemoteObject{ID: "created", Name: "book.epub", Size: 1}}
	s := &unixMTPSession{conn: conn}
	if _, err := s.Upload(context.Background(), "s", "", "book.epub", bytes.NewReader([]byte("ok")), 2, nil); err == nil {
		t.Fatal("expected verification failure")
	}
	if len(conn.deleted) != 1 || conn.deleted[0] != "created" {
		t.Fatalf("partial object not cleaned: %v", conn.deleted)
	}
}
func TestUnixMTPSessionSerializesCalls(t *testing.T) {
	conn := &fakeUnixConn{gate: 20 * time.Millisecond}
	s := &unixMTPSession{conn: conn}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Storages(context.Background()) }()
	}
	wg.Wait()
	if got := conn.maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent backend calls=%d, want 1", got)
	}
}
func TestUnixMTPErrorTranslation(t *testing.T) {
	if got := translateMTPError(errors.New("libusb claim interface busy")); got == nil || got.Error() == "libusb claim interface busy" {
		t.Fatalf("busy error not translated: %v", got)
	}
	if got := translateMTPError(errors.New("no device attached")); got == nil || got.Error() == "no device attached" {
		t.Fatalf("disconnect error not translated: %v", got)
	}
}
