//go:build windows

package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WPDTransport uses the bundled Windows Portable Devices helper. The helper
// keeps all COM work on its main thread and speaks newline-delimited JSON.
type WPDTransport struct {
	helperPath string
}

func NewWPDTransport() *WPDTransport { return &WPDTransport{} }

func (*WPDTransport) ID() string { return "mtp-wpd" }

func platformMTPTransports() []DeviceTransport { return []DeviceTransport{NewWPDTransport()} }

func (t *WPDTransport) helper() (string, error) {
	if t.helperPath != "" {
		return t.helperPath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find Attune executable: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), "wpd-helper.exe"), nil
}

func (t *WPDTransport) start() (*wpdClient, error) {
	path, err := t.helper()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start WPD helper input: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start WPD helper output: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start WPD helper: %w", err)
	}
	return &wpdClient{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func (t *WPDTransport) Discover(ctx context.Context) ([]TransportDevice, error) {
	client, err := t.start()
	if err != nil {
		return nil, err
	}
	defer client.stop()
	var devices []TransportDevice
	if err := client.call(ctx, map[string]any{"op": "discover"}, &devices, nil); err != nil {
		return nil, err
	}
	return devices, nil
}

func (t *WPDTransport) Open(ctx context.Context, id string) (DeviceSession, error) {
	if id == "" {
		return nil, errors.New("WPD device ID is required")
	}
	client, err := t.start()
	if err != nil {
		return nil, err
	}
	if err := client.call(ctx, map[string]any{"op": "open", "device": id}, nil, nil); err != nil {
		client.stop()
		return nil, err
	}
	return &wpdSession{client: client}, nil
}

type wpdClient struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	mu     sync.Mutex
	write  sync.Mutex
	closed bool
}

type wpdEnvelope struct {
	Type   string          `json:"type"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
	Done   uint64          `json:"done"`
	Total  uint64          `json:"total"`
}

func (c *wpdClient) call(ctx context.Context, request any, result any, progress ProgressFunc) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("WPD helper is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Keep paths and object IDs as UTF-8 rather than JSON's optional HTML escapes.
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		return err
	}
	payload := bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'})
	c.write.Lock()
	n, err := c.in.Write(append(payload, '\n'))
	c.write.Unlock()
	if err != nil {
		return fmt.Errorf("send WPD request: %w", err)
	}
	if n != len(payload)+1 {
		return io.ErrShortWrite
	}
	done := make(chan struct{})
	cancelSent := make(chan struct{})
	go func() {
		defer close(cancelSent)
		select {
		case <-ctx.Done():
			c.write.Lock()
			defer c.write.Unlock()
			select {
			case <-done:
				return
			default:
			}
			_, _ = io.WriteString(c.in, "{\"op\":\"cancel\"}\n")
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-cancelSent
	}()
	for {
		line, readErr := c.out.ReadBytes('\n')
		if readErr != nil {
			return fmt.Errorf("read WPD response: %w", readErr)
		}
		var response wpdEnvelope
		if err := json.Unmarshal(line, &response); err != nil {
			return fmt.Errorf("decode WPD response: %w", err)
		}
		if response.Type == "progress" {
			if progress != nil {
				progress(response.Done, response.Total)
			}
			continue
		}
		if !response.OK {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if response.Error == "an object with this name already exists" {
				return fmt.Errorf("%w: %s", ErrObjectExists, response.Error)
			}
			if response.Error == "" {
				response.Error = "WPD operation failed"
			}
			return errors.New(response.Error)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if result != nil && len(response.Result) != 0 && string(response.Result) != "null" {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("decode WPD result: %w", err)
			}
		}
		return nil
	}
}

func (c *wpdClient) stop() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	// Ask the helper to close its WPD session and release COM objects on the
	// owning thread before waiting for process exit.
	_ = c.call(context.Background(), map[string]any{"op": "close"}, nil, nil)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_ = c.in.Close()
	_ = c.cmd.Wait()
}

type wpdSession struct {
	client *wpdClient
	discMu sync.Mutex
	disc   bool
}

func (s *wpdSession) Storages(ctx context.Context) ([]TransportStorage, error) {
	var out []TransportStorage
	err := s.client.call(ctx, map[string]any{"op": "storages"}, &out, nil)
	return out, err
}

func (s *wpdSession) List(ctx context.Context, storageID, parentID string) ([]RemoteObject, error) {
	if storageID == "" {
		return nil, errors.New("WPD storage ID is required")
	}
	var out []RemoteObject
	err := s.client.call(ctx, map[string]any{"op": "list", "storage": storageID, "parent": parentID}, &out, nil)
	return out, err
}

func (s *wpdSession) Download(ctx context.Context, storageID, objectID string, dst io.Writer, progress ProgressFunc) error {
	if storageID == "" || objectID == "" || dst == nil {
		return errors.New("WPD storage, object, and destination are required")
	}
	file, err := os.CreateTemp("", "attune-wpd-download-*")
	if err != nil {
		return err
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)
	if err := s.client.call(ctx, map[string]any{"op": "download", "storage": storageID, "object": objectID, "path": path}, nil, wpdContextProgress(ctx, progress)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err = os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded WPD data: %w", err)
	}
	defer file.Close()
	_, err = io.Copy(dst, file)
	return err
}

func (s *wpdSession) Upload(ctx context.Context, storageID, parentID, name string, src io.Reader, size int64, progress ProgressFunc) (RemoteObject, error) {
	if storageID == "" || name == "" || strings.ContainsAny(name, "/\\\x00") || src == nil || size < 0 {
		return RemoteObject{}, errors.New("valid WPD storage, filename, reader, and size are required")
	}
	file, err := os.CreateTemp("", "attune-wpd-upload-*")
	if err != nil {
		return RemoteObject{}, err
	}
	path := file.Name()
	defer os.Remove(path)
	written, copyErr := copyWithContext(ctx, file, src)
	closeErr := file.Close()
	if copyErr != nil {
		return RemoteObject{}, copyErr
	}
	if closeErr != nil {
		return RemoteObject{}, closeErr
	}
	if written != size {
		return RemoteObject{}, fmt.Errorf("upload size changed while reading: expected %d bytes, read %d", size, written)
	}
	var out RemoteObject
	err = s.client.call(ctx, map[string]any{"op": "upload", "storage": storageID, "parent": parentID, "name": name, "path": path, "size": size}, &out, wpdContextProgress(ctx, progress))
	return out, err
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 64*1024)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		read, readErr := src.Read(buf)
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if read > 0 {
			written, writeErr := dst.Write(buf[:read])
			n += int64(written)
			if writeErr != nil {
				return n, writeErr
			}
			if written != read {
				return n, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return n, nil
			}
			return n, readErr
		}
	}
}

func wpdContextProgress(ctx context.Context, progress ProgressFunc) ProgressFunc {
	if progress == nil {
		return nil
	}
	return func(done, total uint64) {
		if ctx.Err() == nil {
			progress(done, total)
		}
	}
}

func (s *wpdSession) Delete(ctx context.Context, storageID, objectID string) error {
	if storageID == "" || objectID == "" {
		return errors.New("WPD storage and object IDs are required")
	}
	return s.client.call(ctx, map[string]any{"op": "delete", "storage": storageID, "object": objectID}, nil, nil)
}

func (s *wpdSession) Disconnect(ctx context.Context) error {
	s.discMu.Lock()
	defer s.discMu.Unlock()
	if s.disc {
		return nil
	}
	s.disc = true
	err := s.client.call(ctx, map[string]any{"op": "close"}, nil, nil)
	s.client.stop()
	return err
}
