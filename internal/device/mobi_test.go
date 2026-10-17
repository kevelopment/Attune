package device

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseMOBIMetadataAndCover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.mobi")
	cover := testJPEG(t)
	if err := os.WriteFile(path, testMOBI(t, cover), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := parseMOBIMetadata(path, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got.title != "Edited title" || len(got.authors) != 2 || got.authors[0] != "Author One" || got.authors[1] != "Author Two" {
		t.Fatalf("unexpected title/authors: %+v", got)
	}
	if got.language != "en-US" || len(got.subjects) != 1 || got.subjects[0] != "Fantasy" {
		t.Fatalf("unexpected language/subjects: %+v", got)
	}
	if got.publisher != "Example Press" || got.published != "2024-03-10" || got.description != "An example description. & more." || got.identifiers["isbn"] != "9780000000002" || got.identifiers["asin"] != "B012345678" {
		t.Fatalf("unexpected extra metadata: %+v", got)
	}
	const prefix = "data:image/jpeg;base64,"
	if len(got.coverURL) <= len(prefix) || got.coverURL[:len(prefix)] != prefix {
		t.Fatalf("missing cover URI: %q", got.coverURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.coverURL[len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	if typ, _, ok := imageType(decoded); !ok || typ != "jpeg" {
		t.Fatalf("invalid resized cover: %q", typ)
	}
}

func TestDeviceMetadataCacheUsesVolumePathSizeAndMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.mobi")
	if err := os.WriteFile(path, testMOBI(t, testJPEG(t)), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	first := m.readMetadata("volume-a", "documents/fixture.mobi", path, st)
	first.title = "mutated caller copy"
	first.identifiers["isbn"] = "mutated"
	second := m.readMetadata("volume-a", "documents/fixture.mobi", path, st)
	if second.title != "Edited title" || second.identifiers["isbn"] != "9780000000002" {
		t.Fatalf("cached metadata was not isolated: %+v", second)
	}
	otherVolume := m.readMetadata("volume-b", "documents/fixture.mobi", path, st)
	if otherVolume.title != "Edited title" || len(m.meta) != 2 {
		t.Fatalf("volume identity not included in cache key: entries=%d, metadata=%+v", len(m.meta), otherVolume)
	}
}

func TestParseMOBIMetadataFallsBackForNonMOBIPRC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unrelated.prc")
	if err := os.WriteFile(path, []byte("not a MOBI database"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseMOBIMetadata(path, "fallback"); err == nil {
		t.Fatal("non-MOBI PRC should produce filename fallback")
	}
}

func TestDeviceFileTimesUseFilesystemMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.mobi")
	if err := os.WriteFile(path, []byte("book"), 0600); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 2, 3, 4, 5, 6, 123456789, time.UTC)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	modified, added := deviceFileTimes(st)
	if modified != want.Format(time.RFC3339Nano) {
		t.Fatalf("modifiedAt = %q, want %q", modified, want.Format(time.RFC3339Nano))
	}
	if added != "" {
		if _, err := time.Parse(time.RFC3339Nano, added); err != nil {
			t.Fatalf("invalid optional addedAt %q: %v", added, err)
		}
	}
}

func testMOBI(t *testing.T, cover []byte) []byte {
	t.Helper()
	type record struct {
		typ  uint32
		data []byte
	}
	records := []record{{100, []byte("Author One")}, {100, []byte("Author Two")}, {101, []byte("Example Press")}, {103, []byte(`<p>An <b>example</b> description. &amp; more.</p><script>ignored()</script>`)}, {104, []byte("9780000000002")}, {105, []byte("Fantasy")}, {106, []byte("2024-03-10")}, {113, []byte("B012345678")}, {201, []byte{0, 0, 0, 0}}, {503, []byte("Edited title")}, {524, []byte("en-US")}}
	var exth bytes.Buffer
	exth.WriteString("EXTH")
	_ = binary.Write(&exth, binary.BigEndian, uint32(0))
	_ = binary.Write(&exth, binary.BigEndian, uint32(len(records)))
	for _, r := range records {
		_ = binary.Write(&exth, binary.BigEndian, r.typ)
		_ = binary.Write(&exth, binary.BigEndian, uint32(8+len(r.data)))
		exth.Write(r.data)
	}
	exthBytes := exth.Bytes()
	binary.BigEndian.PutUint32(exthBytes[4:8], uint32(len(exthBytes)))
	const mobiHeaderLen = 232
	zero := make([]byte, 16+mobiHeaderLen)
	mobi := zero[16:]
	copy(mobi[:4], "MOBI")
	binary.BigEndian.PutUint32(mobi[4:8], mobiHeaderLen)
	binary.BigEndian.PutUint32(mobi[12:16], 65001)
	binary.BigEndian.PutUint32(mobi[68:72], uint32(len(zero)+len(exthBytes)))
	binary.BigEndian.PutUint32(mobi[72:76], uint32(len("Embedded title")))
	binary.BigEndian.PutUint32(mobi[76:80], 0x09)
	binary.BigEndian.PutUint32(mobi[92:96], 1)
	binary.BigEndian.PutUint32(mobi[112:116], 0x40)
	zero = append(zero, exthBytes...)
	zero = append(zero, []byte("Embedded title")...)
	recordZeroOffset := uint32(78 + 2*8)
	imageOffset := recordZeroOffset + uint32(len(zero))
	file := make([]byte, 78+2*8)
	copy(file[:32], "Test PDB")
	binary.BigEndian.PutUint16(file[76:78], 2)
	binary.BigEndian.PutUint32(file[78:82], recordZeroOffset)
	binary.BigEndian.PutUint32(file[86:90], imageOffset)
	file = append(file, zero...)
	file = append(file, cover...)
	return file
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 6), B: 100, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
