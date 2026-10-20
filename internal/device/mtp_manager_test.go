package device

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type managerMTPTransport struct{ session *managerMTPSession }

func (t *managerMTPTransport) ID() string { return "test-mtp" }
func (t *managerMTPTransport) Discover(context.Context) ([]TransportDevice, error) {
	return []TransportDevice{{ID: "device-opaque", Name: "Kobo Clara"}}, nil
}
func (t *managerMTPTransport) Open(context.Context, string) (DeviceSession, error) {
	return t.session, nil
}

type managerMTPSession struct {
	objects  map[string][]RemoteObject
	uploaded []byte
	deleted  []string
}

func (s *managerMTPSession) Storages(context.Context) ([]TransportStorage, error) {
	return []TransportStorage{{ID: "storage-opaque", Name: "Internal", FreeBytes: 1 << 20}}, nil
}
func (s *managerMTPSession) List(_ context.Context, _, parent string) ([]RemoteObject, error) {
	return append([]RemoteObject(nil), s.objects[parent]...), nil
}
func (s *managerMTPSession) Download(_ context.Context, _, _ string, dst io.Writer, _ ProgressFunc) error {
	_, err := dst.Write([]byte("data"))
	return err
}
func (s *managerMTPSession) Upload(_ context.Context, _, parent, name string, src io.Reader, size int64, _ ProgressFunc) (RemoteObject, error) {
	for _, object := range s.objects[parent] {
		if object.Name == name {
			return RemoteObject{}, ErrObjectExists
		}
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return RemoteObject{}, err
	}
	s.uploaded = data
	object := RemoteObject{ID: "new-object-opaque", Name: name, Size: int64(len(data))}
	s.objects[parent] = append(s.objects[parent], object)
	return object, nil
}
func (s *managerMTPSession) Delete(_ context.Context, _, id string) error {
	s.deleted = append(s.deleted, id)
	for parent, objects := range s.objects {
		for i, object := range objects {
			if object.ID == id {
				s.objects[parent] = append(objects[:i], objects[i+1:]...)
				return nil
			}
		}
	}
	return nil
}
func (*managerMTPSession) Disconnect(context.Context) error { return nil }

func newManagerMTPFixture() (*Manager, *managerMTPSession) {
	session := &managerMTPSession{objects: map[string][]RemoteObject{
		"": {{ID: "books-folder-opaque", Name: "Books", Directory: true}},
		"books-folder-opaque": {
			{ID: "nested-folder-opaque", Name: "Fiction", Directory: true},
			{ID: "existing-book-opaque", Name: "existing.epub", Size: 4},
		},
		"nested-folder-opaque": {{ID: "nested-book-opaque", Name: "nested.pdf", Size: 8}},
	}}
	transport := &managerMTPTransport{session: session}
	manager := &Manager{
		volumes:    func() ([]volume, error) { return nil, nil },
		transports: map[string]DeviceTransport{transport.ID(): transport},
	}
	return manager, session
}

func TestMTPRefreshCachesDevicesForFastDiscoverAndListsConfiguredSubtree(t *testing.T) {
	m, _ := newManagerMTPFixture()
	changed, err := m.RefreshTransports(context.Background())
	if err != nil || !changed {
		t.Fatalf("RefreshTransports() = changed %v, err %v", changed, err)
	}
	devices, err := m.Discover()
	if err != nil || len(devices) != 1 {
		t.Fatalf("Discover() = %+v, %v", devices, err)
	}
	d := devices[0]
	if d.Transport != "test-mtp" || d.StorageID != "storage-opaque" || d.ProfileID != "kobo" {
		t.Fatalf("unexpected device identity/profile: %+v", d)
	}
	books, err := m.List(d)
	if err != nil || len(books) != 2 {
		t.Fatalf("List() = %+v, %v", books, err)
	}
	want := map[string]bool{"existing-book-opaque": true, "nested-book-opaque": true}
	for _, book := range books {
		if !want[book.Path] {
			t.Errorf("opaque object identity was not preserved: %+v", book)
		}
		if !book.CanRead {
			t.Errorf("readable format not marked readable: %+v", book)
		}
	}
}

func TestMTPTransferUsesOpaqueFolderAndObjectIDsWithoutOverwrite(t *testing.T) {
	m, session := newManagerMTPFixture()
	_, _ = m.RefreshTransports(context.Background())
	devices, _ := m.Discover()
	d := devices[0]
	// Select the test profile's actual directory rather than relying on its
	// Kindle label: transfer profiles and transports are independent.
	d.ProfileID = "kobo"
	d.BookFolders = []string{"Books"}
	d.TransferFormats = []string{"epub", "pdf"}
	source := t.TempDir() + "/book.epub"
	if err := os.WriteFile(source, []byte("book bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(context.Background(), d, source, "new.epub"); err != nil {
		t.Fatal(err)
	}
	if string(session.uploaded) != "book bytes" {
		t.Fatalf("uploaded bytes=%q", session.uploaded)
	}
	if _, err := m.Send(context.Background(), d, source, "existing.epub"); err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("existing object was not protected: %v", err)
	}
	if err := m.Remove(d, "new-object-opaque"); err != nil {
		t.Fatal(err)
	}
	if len(session.deleted) != 1 || session.deleted[0] != "new-object-opaque" {
		t.Fatalf("deleted object IDs=%v", session.deleted)
	}
}

func TestMTPRefreshRetainsLastSeenDevicesWhenTransportIsBusy(t *testing.T) {
	m, _ := newManagerMTPFixture()
	_, _ = m.RefreshTransports(context.Background())
	transport := m.transports["test-mtp"].(*managerMTPTransport)
	// Make a transport-level discovery error without changing the cached entry.
	transportErr := &failingMTPTransport{err: context.DeadlineExceeded}
	m.transports[transport.ID()] = transportErr
	changed, err := m.RefreshTransports(context.Background())
	if changed || err == nil {
		t.Fatalf("failed refresh changed cache=%v err=%v", changed, err)
	}
	devices, discoverErr := m.Discover()
	if discoverErr != nil || len(devices) != 1 {
		t.Fatalf("cached device disappeared after transient error: %+v, %v", devices, discoverErr)
	}
}

type failingMTPTransport struct{ err error }

func (*failingMTPTransport) ID() string { return "test-mtp" }
func (t *failingMTPTransport) Discover(context.Context) ([]TransportDevice, error) {
	return nil, t.err
}
func (*failingMTPTransport) Open(context.Context, string) (DeviceSession, error) {
	return nil, errors.New("not used")
}

var _ DeviceSession = (*managerMTPSession)(nil)
var _ DeviceTransport = (*managerMTPTransport)(nil)
