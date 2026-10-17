package converter

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func init() {
	if os.Getenv("ATTUNE_CONVERTER_HELPER") == "" {
		return
	}
	mode := os.Getenv("ATTUNE_CONVERTER_HELPER")
	switch mode {
	case "fake":
		if len(os.Args) > 1 && os.Args[1] == "--version" {
			fmt.Fprintln(os.Stdout, "kindling test")
			os.Exit(0)
		}
		log, err := os.OpenFile(os.Getenv("ATTUNE_CONVERTER_ARGS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		for _, arg := range os.Args[1:] {
			fmt.Fprintln(log, arg)
		}
		log.Close()
		for i := 1; i+1 < len(os.Args); i++ {
			if os.Args[i] == "-o" {
				if err := copyFile(os.Getenv("ATTUNE_CONVERTER_FIXTURE"), os.Args[i+1]); err != nil {
					os.Exit(3)
				}
				os.Exit(0)
			}
		}
		os.Exit(4)
	case "slow":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	case "noisy":
		_, _ = io.CopyN(os.Stdout, strings.NewReader(strings.Repeat("1234567890", 110000)), 1<<30)
		os.Exit(0)
	default:
		os.Exit(5)
	}
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeEPUB(t *testing.T, path, version string) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"OEBPS/book.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" unique-identifier="id" version="` + version + `"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">urn:test:book</dc:identifier><dc:title>Fixture</dc:title><dc:creator>Author</dc:creator><dc:language>en</dc:language></metadata><manifest><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/></manifest><spine toc="ncx"><itemref idref="chapter"/></spine></package>`},
		{"OEBPS/chapter.xhtml", `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter</title></head><body><h1>Fixture chapter</h1><p>Kindling conversion test.</p></body></html>`},
		{"OEBPS/nav.xhtml", `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>Contents</title></head><body><nav epub:type="toc"><ol><li><a href="chapter.xhtml">Chapter</a></li></ol></nav></body></html>`},
		{"OEBPS/toc.ncx", `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><head/><docTitle><text>Fixture</text></docTitle><navMap><navPoint id="c1" playOrder="1"><navLabel><text>Chapter</text></navLabel><content src="chapter.xhtml"/></navPoint></navMap></ncx>`},
	}
	for i, x := range files {
		h := &zip.FileHeader{Name: x.name, Method: zip.Deflate}
		if i == 0 {
			h.Method = zip.Store
		}
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(x.body)); e != nil {
			t.Fatal(e)
		}
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestValidateLegacyMOBIRequiresVersion6(t *testing.T) {
	capbuf := &cappedBuffer{}
	_, _ = capbuf.Write(make([]byte, maxOutput+100))
	if len(capbuf.Bytes()) != maxOutput || !capbuf.exceeded() {
		t.Fatalf("cap buffer len=%d exceeded=%v", len(capbuf.Bytes()), capbuf.exceeded())
	}
	for _, version := range []uint32{6, 8} {
		p := filepath.Join(t.TempDir(), "fixture.mobi")
		data := make([]byte, 400)
		binary.BigEndian.PutUint16(data[76:78], 2)
		binary.BigEndian.PutUint32(data[78:82], 100)
		copy(data[116:120], "MOBI")
		binary.BigEndian.PutUint32(data[120:124], 264)
		binary.BigEndian.PutUint32(data[136:140], version)
		if e := os.WriteFile(p, data, 0600); e != nil {
			t.Fatal(e)
		}
		err := validateLegacyMOBI(p)
		if (version == 6) != (err == nil) {
			t.Fatalf("version %d: %v", version, err)
		}
	}
}

func TestRunnerPassesLegacyAndMetadataArguments(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.epub")
	if err := os.WriteFile(source, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "valid.mobi")
	mobi := make([]byte, 400)
	binary.BigEndian.PutUint16(mobi[76:78], 2)
	binary.BigEndian.PutUint32(mobi[78:82], 100)
	copy(mobi[116:120], "MOBI")
	binary.BigEndian.PutUint32(mobi[120:124], 264)
	binary.BigEndian.PutUint32(mobi[136:140], 6)
	if err := os.WriteFile(fixture, mobi, 0600); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(dir, "args.txt")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	withHelper := func(mode string) *Runner {
		t.Setenv("ATTUNE_CONVERTER_HELPER", mode)
		t.Setenv("ATTUNE_CONVERTER_ARGS", argsLog)
		t.Setenv("ATTUNE_CONVERTER_FIXTURE", fixture)
		return New(bin)
	}
	output := filepath.Join(dir, "converted.mobi")
	if err := withHelper("fake").Convert(context.Background(), source, output, "Edited Title", []string{"Author One", "Author Two"}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"build", "--legacy-mobi", "--no-validate", "rewrite-metadata", "Edited Title", "Author One", "Author Two"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("arguments missing %q: %s", want, args)
		}
	}
}

func TestRunnerHonorsCancellationAndOutputLimit(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	makeRunner := func(mode string) *Runner {
		t.Setenv("ATTUNE_CONVERTER_HELPER", mode)
		return New(bin)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := makeRunner("slow").Version(ctx); err == nil || !strings.Contains(err.Error(), "cancelled or timed out") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	got, err := makeRunner("noisy").Version(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeded 1 MiB") {
		t.Fatalf("expected bounded-output error, got %v (version output bytes %d)", err, len(got))
	}
}

func TestBundledKindlingConvertsEPUB2AndEPUB3(t *testing.T) {
	name := "kindling-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binaryPath := filepath.Join("..", "..", "third_party", "kindling", runtime.GOOS+"-"+runtime.GOARCH, name)
	if _, e := os.Stat(binaryPath); e != nil {
		t.Skip("bundled Kindling binary unavailable")
	}
	r := New(binaryPath)
	version, e := r.Version(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(version, "0.45.6") {
		t.Fatalf("unexpected version: %s", version)
	}
	for _, v := range []string{"2.0", "3.0"} {
		t.Run("epub"+v, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "input.epub")
			out := filepath.Join(t.TempDir(), "output.mobi")
			writeEPUB(t, src, v)
			if e := r.Convert(context.Background(), src, out, "Edited Title", []string{"First Author", "Second Author"}); e != nil {
				t.Fatal(e)
			}
			if e := validateLegacyMOBI(out); e != nil {
				t.Fatal(e)
			}
			dump, e := r.run(context.Background(), "dump", out)
			if e != nil {
				t.Fatal(e)
			}
			for _, want := range []string{"Edited Title", "First Author", "Second Author"} {
				if !strings.Contains(string(dump), want) {
					t.Fatalf("metadata dump does not contain %q: %s", want, dump)
				}
			}
		})
	}
}
