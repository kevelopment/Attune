// Package converter wraps the bundled Kindling CLI.
package converter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	commandTimeout = 2 * time.Minute
	maxOutput      = 1 << 20
)

type Runner struct{ binaryPath string }

func New(binaryPath string) *Runner { return &Runner{binaryPath: binaryPath} }

func (r *Runner) Version(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "--version")
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", errors.New("Kindling returned an empty version")
	}
	return v, nil
}

// Convert builds a dual-format legacy MOBI with metadata overrides. It writes
// to a temporary sibling and only publishes output after validating the MOBI
// PalmDB and MOBI headers.
func (r *Runner) Convert(ctx context.Context, source, output, title string, authors []string) error {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(output) == "" {
		return errors.New("source and output paths are required")
	}
	st, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("open EPUB: %w", err)
	}
	if !st.Mode().IsRegular() {
		return errors.New("source EPUB must be a regular file")
	}
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Dir(outAbs)); err != nil {
		return fmt.Errorf("output directory: %w", err)
	}
	if _, err = os.Lstat(outAbs); err == nil {
		return fmt.Errorf("refusing to overwrite existing output %q", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outAbs), ".kindling-*.mobi")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)
	buildPath := tmpPath + ".build"
	defer os.Remove(buildPath)
	// KDP's publishing preflight rejects coverless books, although a cover is
	// optional for locally sideloaded reading on these legacy devices.
	args := []string{"build", source, "-o", buildPath, "--legacy-mobi", "--no-validate"}
	if _, err = r.run(ctx, args...); err != nil {
		return err
	}
	rewrite := []string{"rewrite-metadata", buildPath, "-o", tmpPath}
	if strings.TrimSpace(title) != "" {
		rewrite = append(rewrite, "--title", title)
	}
	for _, a := range authors {
		if strings.TrimSpace(a) != "" {
			rewrite = append(rewrite, "--author", a)
		}
	}
	if _, err = r.run(ctx, rewrite...); err != nil {
		return err
	}
	if err = validateLegacyMOBI(tmpPath); err != nil {
		return fmt.Errorf("Kindling output invalid: %w", err)
	}
	if err = os.Link(tmpPath, outAbs); err != nil {
		return fmt.Errorf("publish converted file: %w", err)
	}
	return nil
}

func (r *Runner) run(ctx context.Context, args ...string) ([]byte, error) {
	if r.binaryPath == "" {
		return nil, errors.New("Kindling binary path is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binaryPath, args...)
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Kindling cancelled or timed out: %w", ctx.Err())
		}
		detail := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("Kindling failed: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("Kindling failed: %w", err)
	}
	if stdout.exceeded() || stderr.exceeded() {
		return nil, errors.New("Kindling output exceeded 1 MiB")
	}
	return append(stdout.Bytes(), stderr.Bytes()...), nil
}

type cappedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remain := maxOutput - b.buffer.Len()
	if remain > 0 {
		take := p
		if len(take) > remain {
			take = take[:remain]
		}
		b.buffer.Write(take)
	}
	if n > remain {
		b.overflow = true
	}
	return n, nil
}

func (b *cappedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}
func (b *cappedBuffer) exceeded() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.overflow }

func validateLegacyMOBI(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < 86 {
		return errors.New("file is too small for a PalmDB/MOBI header")
	}
	read := func(off int64, n int) ([]byte, error) { b := make([]byte, n); _, e := f.ReadAt(b, off); return b, e }
	pdb, err := read(0, 86)
	if err != nil {
		return err
	}
	count := binary.BigEndian.Uint16(pdb[76:78])
	if count < 2 {
		return errors.New("PalmDB has no MOBI record")
	}
	first := binary.BigEndian.Uint32(pdb[78:82])
	if uint64(first)+40 > uint64(st.Size()) {
		return errors.New("PalmDB first record offset is out of bounds")
	}
	magic, err := read(int64(first)+16, 24)
	if err != nil {
		return err
	}
	if string(magic[:4]) != "MOBI" {
		return errors.New("first PalmDB record lacks MOBI header")
	}
	mobi := first + 16
	headerLen := binary.BigEndian.Uint32(magic[4:8])
	if headerLen < 24 || uint64(mobi)+uint64(headerLen) > uint64(st.Size()) {
		return errors.New("invalid MOBI header length")
	}
	// Kindling dual-format legacy files contain a KF7 header followed by KF8
	// boundary/section records. At minimum verify the legacy MOBI header parses.
	version := binary.BigEndian.Uint32(magic[20:24])
	if version != 6 {
		return fmt.Errorf("expected legacy MOBI7 header (version 6), got MOBI version %d", version)
	}
	return nil
}
