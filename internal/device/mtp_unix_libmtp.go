//go:build (darwin || linux) && libmtp && cgo

package device

/*
#cgo pkg-config: libmtp
#include <libmtp.h>
#include <stdlib.h>
#include <stdint.h>
#include "mtp_libmtp_progress.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/cgo"
	"strconv"
	"strings"
	"unsafe"
)

type libmtpBackend struct{}
type libmtpSession struct{ dev *C.LIBMTP_mtpdevice_t }

type mtpProgressState struct {
	ctx      context.Context
	callback ProgressFunc
}

//export goAttuneMTPProgress
func goAttuneMTPProgress(done C.uint64_t, total C.uint64_t, handle C.uintptr_t) C.int {
	state, ok := cgo.Handle(handle).Value().(mtpProgressState)
	if !ok {
		return 0
	}
	if state.ctx != nil && state.ctx.Err() != nil {
		return 1
	}
	if state.callback != nil {
		state.callback(uint64(done), uint64(total))
	}
	if state.ctx != nil && state.ctx.Err() != nil {
		return 1
	}
	return 0
}

func makeMTPProgress(ctx context.Context, progress ProgressFunc) (*C.attune_mtp_progress_context, func()) {
	if progress == nil && (ctx == nil || ctx.Done() == nil) {
		return nil, func() {}
	}
	handle := cgo.NewHandle(mtpProgressState{ctx: ctx, callback: progress})
	data := (*C.attune_mtp_progress_context)(C.malloc(C.sizeof_attune_mtp_progress_context))
	if data == nil {
		handle.Delete()
		return nil, func() {}
	}
	data.handle = C.uintptr_t(handle)
	return data, func() {
		C.free(unsafe.Pointer(data))
		handle.Delete()
	}
}

func newUnixMTPBackend() unixMTPBackend        { C.LIBMTP_Init(); return libmtpBackend{} }
func platformMTPTransports() []DeviceTransport { return []DeviceTransport{NewUnixMTPTransport()} }

func rawDevices() (*C.LIBMTP_raw_device_t, C.int, error) {
	var raw *C.LIBMTP_raw_device_t
	var count C.int
	if C.LIBMTP_Detect_Raw_Devices(&raw, &count) != C.LIBMTP_ERROR_NONE {
		if raw != nil {
			C.free(unsafe.Pointer(raw))
		}
		return nil, 0, errors.New("libmtp could not enumerate MTP devices")
	}
	return raw, count, nil
}
func rawID(raw *C.LIBMTP_raw_device_t) string {
	return fmt.Sprintf("%04x:%04x:%d:%d", uint16(raw.device_entry.vendor_id), uint16(raw.device_entry.product_id), uint32(raw.bus_location), uint8(raw.devnum))
}
func cstring(p *C.char) string {
	if p == nil {
		return ""
	}
	return C.GoString(p)
}
func (libmtpBackend) Discover(ctx context.Context) ([]TransportDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, count, err := rawDevices()
	if err != nil {
		return nil, err
	}
	defer C.free(unsafe.Pointer(raw))
	out := make([]TransportDevice, 0, int(count))
	for i := 0; i < int(count); i++ {
		d := (*C.LIBMTP_raw_device_t)(unsafe.Add(unsafe.Pointer(raw), uintptr(i)*unsafe.Sizeof(*raw)))
		out = append(out, TransportDevice{ID: rawID(d), Name: cstring(d.device_entry.product), Vendor: cstring(d.device_entry.vendor), Product: cstring(d.device_entry.product)})
	}
	return out, ctx.Err()
}
func (libmtpBackend) Open(ctx context.Context, id string) (unixMTPConnection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, count, err := rawDevices()
	if err != nil {
		return nil, err
	}
	defer C.free(unsafe.Pointer(raw))
	for i := 0; i < int(count); i++ {
		d := (*C.LIBMTP_raw_device_t)(unsafe.Add(unsafe.Pointer(raw), uintptr(i)*unsafe.Sizeof(*raw)))
		if rawID(d) == id {
			dev := C.LIBMTP_Open_Raw_Device_Uncached(d)
			if dev == nil {
				return nil, errors.New("libmtp could not open device; it may be busy or disconnected")
			}
			return &libmtpSession{dev: dev}, nil
		}
	}
	return nil, errors.New("MTP device disappeared; reconnect it and retry")
}
func parseObjectID(id string) (uint32, error) {
	n, e := strconv.ParseUint(id, 10, 32)
	if e != nil || id == "" {
		return 0, errors.New("invalid opaque MTP object ID")
	}
	return uint32(n), nil
}
func parseStorageID(id string) (uint32, error) { return parseObjectID(id) }
func parentID(id string) (uint32, error) {
	if id == "" {
		return uint32(C.LIBMTP_FILES_AND_FOLDERS_ROOT), nil
	}
	return parseObjectID(id)
}
func (s *libmtpSession) Storages(ctx context.Context) ([]TransportStorage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if C.LIBMTP_Get_Storage(s.dev, C.LIBMTP_STORAGE_SORTBY_NOTSORTED) != 0 {
		return nil, libmtpError(s.dev, "read MTP storage list")
	}
	var out []TransportStorage
	for st := s.dev.storage; st != nil; st = st.next {
		out = append(out, TransportStorage{ID: strconv.FormatUint(uint64(st.id), 10), Name: first(cstring(st.VolumeIdentifier), cstring(st.StorageDescription)), FreeBytes: uint64(st.FreeSpaceInBytes), TotalBytes: uint64(st.MaxCapacity)})
	}
	return out, ctx.Err()
}
func (s *libmtpSession) List(ctx context.Context, storage, parent string) ([]RemoteObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sid, e := parseStorageID(storage)
	if e != nil {
		return nil, e
	}
	pid, e := parentID(parent)
	if e != nil {
		return nil, e
	}
	files := C.LIBMTP_Get_Files_And_Folders(s.dev, C.uint32_t(sid), C.uint32_t(pid))
	if files == nil {
		return nil, libmtpError(s.dev, "list MTP folder")
	}
	var out []RemoteObject
	for f := files; f != nil; {
		next := f.next
		out = append(out, RemoteObject{ID: strconv.FormatUint(uint64(f.item_id), 10), Name: cstring(f.filename), Size: int64(f.filesize), Directory: f.filetype == C.LIBMTP_FILETYPE_FOLDER})
		C.LIBMTP_destroy_file_t(f)
		f = next
	}
	return out, ctx.Err()
}
func (s *libmtpSession) Download(ctx context.Context, storage, object string, dst io.Writer, progress ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dst == nil {
		return errors.New("download destination is nil")
	}
	if _, e := parseStorageID(storage); e != nil {
		return e
	}
	oid, e := parseObjectID(object)
	if e != nil {
		return e
	}
	meta := C.LIBMTP_Get_Filemetadata(s.dev, C.uint32_t(oid))
	if meta == nil {
		return libmtpError(s.dev, "read MTP object metadata")
	}
	want := uint64(meta.filesize)
	C.LIBMTP_destroy_file_t(meta)
	tmp, e := os.CreateTemp("", "attune-mtp-download-*")
	if e != nil {
		return e
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	progressData, cleanupProgress := makeMTPProgress(ctx, progress)
	defer cleanupProgress()
	if C.attune_mtp_get_file(s.dev, C.uint32_t(oid), cname, progressData) != 0 {
		return libmtpError(s.dev, "download MTP object")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f, e := os.Open(name)
	if e != nil {
		return e
	}
	defer f.Close()
	buf := make([]byte, 128*1024)
	var done uint64
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, re := f.Read(buf)
		if n > 0 {
			wn, we := dst.Write(buf[:n])
			done += uint64(wn)
			if we != nil {
				return we
			}
			if wn != n {
				return io.ErrShortWrite
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if done != want {
		return fmt.Errorf("MTP download size mismatch: expected %d bytes, received %d", want, done)
	}
	return nil
}
func (s *libmtpSession) Upload(ctx context.Context, storage, parent, filename string, src io.Reader, size int64, progress ProgressFunc) (RemoteObject, error) {
	if err := ctx.Err(); err != nil {
		return RemoteObject{}, err
	}
	sid, e := parseStorageID(storage)
	if e != nil {
		return RemoteObject{}, e
	}
	pid, e := parentID(parent)
	if e != nil {
		return RemoteObject{}, e
	}
	local, e := os.CreateTemp("", "attune-mtp-upload-*")
	if e != nil {
		return RemoteObject{}, e
	}
	tmp := local.Name()
	defer os.Remove(tmp)
	buf := make([]byte, 128*1024)
	var copied int64
	for copied <= size {
		if e = ctx.Err(); e != nil {
			local.Close()
			return RemoteObject{}, e
		}
		limit := int64(len(buf))
		if left := size + 1 - copied; left < limit {
			limit = left
		}
		n, re := src.Read(buf[:limit])
		if n > 0 {
			wn, we := local.Write(buf[:n])
			copied += int64(wn)
			if we != nil {
				local.Close()
				return RemoteObject{}, we
			}
			if wn != n {
				local.Close()
				return RemoteObject{}, io.ErrShortWrite
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			local.Close()
			return RemoteObject{}, re
		}
		if n == 0 {
			local.Close()
			return RemoteObject{}, io.ErrNoProgress
		}
	}
	if copied != size {
		local.Close()
		return RemoteObject{}, fmt.Errorf("MTP upload source size mismatch: expected %d bytes, read %d", size, copied)
	}
	if e = local.Close(); e != nil {
		return RemoteObject{}, e
	}
	if e = ctx.Err(); e != nil {
		return RemoteObject{}, e
	}
	cpath, cname := C.CString(tmp), C.CString(filename)
	defer C.free(unsafe.Pointer(cpath))
	defer C.free(unsafe.Pointer(cname))
	f := C.LIBMTP_new_file_t()
	if f == nil {
		return RemoteObject{}, errors.New("allocate MTP upload metadata failed")
	}
	defer C.LIBMTP_destroy_file_t(f)
	f.filename = cname
	f.filesize = C.uint64_t(size)
	f.storage_id = C.uint32_t(sid)
	f.parent_id = C.uint32_t(pid)
	f.filetype = C.LIBMTP_FILETYPE_UNKNOWN
	progressData, cleanupProgress := makeMTPProgress(ctx, progress)
	defer cleanupProgress()
	if C.attune_mtp_send_file(s.dev, cpath, f, progressData) != 0 {
		var partial RemoteObject
		if f.item_id != 0 {
			partial = RemoteObject{ID: strconv.FormatUint(uint64(f.item_id), 10), Name: filename, Size: int64(f.filesize)}
		}
		return partial, libmtpError(s.dev, "upload MTP object")
	}
	if err := ctx.Err(); err != nil {
		if f.item_id != 0 {
			return RemoteObject{ID: strconv.FormatUint(uint64(f.item_id), 10), Name: filename, Size: size}, err
		}
		return RemoteObject{}, err
	}
	meta := C.LIBMTP_Get_Filemetadata(s.dev, f.item_id)
	if meta == nil {
		return RemoteObject{ID: strconv.FormatUint(uint64(f.item_id), 10), Name: filename, Size: size}, libmtpError(s.dev, "verify uploaded MTP object")
	}
	defer C.LIBMTP_destroy_file_t(meta)
	return RemoteObject{ID: strconv.FormatUint(uint64(meta.item_id), 10), Name: cstring(meta.filename), Size: int64(meta.filesize)}, nil
}
func (s *libmtpSession) Delete(ctx context.Context, storage, object string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, e := parseStorageID(storage); e != nil {
		return e
	}
	oid, e := parseObjectID(object)
	if e != nil {
		return e
	}
	if C.LIBMTP_Delete_Object(s.dev, C.uint32_t(oid)) != 0 {
		return libmtpError(s.dev, "delete MTP object")
	}
	return ctx.Err()
}
func (s *libmtpSession) Disconnect(ctx context.Context) error {
	if s.dev != nil {
		C.LIBMTP_Release_Device(s.dev)
		s.dev = nil
	}
	return ctx.Err()
}
func libmtpError(dev *C.LIBMTP_mtpdevice_t, operation string) error {
	var messages []string
	for e := C.LIBMTP_Get_Errorstack(dev); e != nil; e = e.next {
		if e.error_text != nil {
			messages = append(messages, cstring(e.error_text))
		}
	}
	C.LIBMTP_Clear_Errorstack(dev)
	if len(messages) == 0 {
		messages = append(messages, "device busy, locked, or disconnected")
	}
	return fmt.Errorf("%s: %s", operation, strings.Join(messages, "; "))
}
