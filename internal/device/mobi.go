package device

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	stdhtml "html"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

const (
	maxPDBRecords  = 8192
	maxRecordZero  = 8 << 20
	maxCoverBytes  = 4 << 20
	maxCoverPixels = 20_000_000
)

type metadataCacheKey struct {
	volumeID string
	path     string
	size     int64
	mtime    int64
}

type deviceMetadata struct {
	title       string
	authors     []string
	language    string
	subjects    []string
	coverURL    string
	publisher   string
	published   string
	identifiers map[string]string
	description string
}

func (m *Manager) readMetadata(volumeID, rel, path string, st os.FileInfo) deviceMetadata {
	key := metadataCacheKey{volumeID: volumeID, path: rel, size: st.Size(), mtime: st.ModTime().UnixNano()}
	m.metaMu.Lock()
	if meta, ok := m.meta[key]; ok {
		m.metaMu.Unlock()
		return cloneDeviceMetadata(meta)
	}
	m.metaMu.Unlock()

	meta := deviceMetadata{title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	if parsed, err := parseMOBIMetadata(path, meta.title); err == nil {
		meta = parsed
	}
	m.metaMu.Lock()
	if m.meta == nil {
		m.meta = make(map[metadataCacheKey]deviceMetadata)
	}
	m.meta[key] = cloneDeviceMetadata(meta)
	m.metaMu.Unlock()
	return meta
}

func cloneDeviceMetadata(m deviceMetadata) deviceMetadata {
	m.authors = append([]string(nil), m.authors...)
	m.subjects = append([]string(nil), m.subjects...)
	if m.identifiers != nil {
		ids := make(map[string]string, len(m.identifiers))
		for key, value := range m.identifiers {
			ids[key] = value
		}
		m.identifiers = ids
	}
	return m
}

func deviceFileTimes(st os.FileInfo) (modifiedAt, addedAt string) {
	if st == nil {
		return "", ""
	}
	modifiedAt = st.ModTime().UTC().Format(time.RFC3339Nano)
	// On macOS, FileInfo.Sys() is *syscall.Stat_t and exposes Birthtimespec.
	// Use reflection so this package remains buildable on other Go platforms.
	v := reflect.ValueOf(st.Sys())
	if v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return modifiedAt, ""
	}
	birth := v.FieldByName("Birthtimespec")
	if !birth.IsValid() || birth.Kind() != reflect.Struct {
		return modifiedAt, ""
	}
	sec, nsec := birth.FieldByName("Sec"), birth.FieldByName("Nsec")
	if !sec.IsValid() || !nsec.IsValid() || !sec.CanInt() || !nsec.CanInt() {
		return modifiedAt, ""
	}
	t := time.Unix(sec.Int(), nsec.Int()).UTC()
	if sec.Int() > 0 && !t.IsZero() {
		addedAt = t.Format(time.RFC3339Nano)
	}
	return modifiedAt, addedAt
}

// parseMOBIMetadata reads only PDB/MOBI headers and a bounded cover record. It
// does not inspect or decrypt book text, so DRM-protected books remain protected.
func parseMOBIMetadata(path, fallbackTitle string) (deviceMetadata, error) {
	meta := deviceMetadata{title: fallbackTitle}
	f, err := os.Open(path)
	if err != nil {
		return meta, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return meta, err
	}
	if !st.Mode().IsRegular() || st.Size() < 86 {
		return meta, fmt.Errorf("not a Palm database")
	}
	var pdb [78]byte
	if _, err = f.ReadAt(pdb[:], 0); err != nil {
		return meta, err
	}
	count := int(binary.BigEndian.Uint16(pdb[76:78]))
	if count < 1 || count > maxPDBRecords || int64(78+count*8) > st.Size() {
		return meta, fmt.Errorf("invalid Palm database record table")
	}
	table := make([]byte, count*8)
	if _, err = f.ReadAt(table, 78); err != nil {
		return meta, err
	}
	offsets := make([]uint32, count)
	for i := 0; i < count; i++ {
		offsets[i] = binary.BigEndian.Uint32(table[i*8 : i*8+4])
		if int64(offsets[i]) >= st.Size() || (i > 0 && offsets[i] <= offsets[i-1]) {
			return meta, fmt.Errorf("invalid Palm database record offset")
		}
	}
	if int64(offsets[0]) < int64(78+count*8) {
		return meta, fmt.Errorf("Palm database record overlaps its header")
	}
	recordZeroEnd := st.Size()
	if count > 1 {
		recordZeroEnd = int64(offsets[1])
	}
	recordZeroLen := recordZeroEnd - int64(offsets[0])
	if recordZeroLen < 16+24 || recordZeroLen > maxRecordZero {
		return meta, fmt.Errorf("invalid MOBI record length")
	}
	recordZero := make([]byte, int(recordZeroLen))
	if _, err = f.ReadAt(recordZero, int64(offsets[0])); err != nil {
		return meta, err
	}
	if string(recordZero[16:20]) != "MOBI" {
		return meta, fmt.Errorf("Palm database has no MOBI header")
	}
	mobi := recordZero[16:]
	headerLen := int(binary.BigEndian.Uint32(mobi[4:8]))
	if headerLen < 24 || headerLen > len(mobi) {
		return meta, fmt.Errorf("invalid MOBI header length")
	}
	header := mobi[:headerLen]
	codePage := uint32(1252)
	if headerLen >= 16 {
		codePage = binary.BigEndian.Uint32(header[12:16])
	}
	if headerLen >= 76 {
		fullNameOffset := int(binary.BigEndian.Uint32(header[68:72]))
		fullNameLen := int(binary.BigEndian.Uint32(header[72:76]))
		if fullNameLen > 0 && fullNameLen <= 4096 && fullNameOffset >= 0 && fullNameOffset <= len(recordZero)-fullNameLen {
			if title := decodeMOBIText(recordZero[fullNameOffset:fullNameOffset+fullNameLen], codePage); title != "" {
				meta.title = title
			}
		}
		meta.language = mobiLanguage(binary.BigEndian.Uint32(header[76:80]) & 0xff)
	}
	exthOffset := 16 + headerLen
	var exth map[uint32][][]byte
	if headerLen >= 116 && binary.BigEndian.Uint32(header[112:116])&0x40 != 0 && exthOffset <= len(recordZero)-12 && string(recordZero[exthOffset:exthOffset+4]) == "EXTH" {
		exth, err = parseEXTH(recordZero[exthOffset:])
		if err != nil {
			return meta, err
		}
	}
	if values := exth[503]; len(values) > 0 {
		if title := decodeMOBIText(values[0], codePage); title != "" {
			meta.title = title
		}
	}
	for _, value := range exth[100] {
		if author := decodeMOBIText(value, codePage); author != "" {
			meta.authors = append(meta.authors, author)
		}
	}
	for _, value := range exth[105] {
		if subject := decodeMOBIText(value, codePage); subject != "" {
			meta.subjects = append(meta.subjects, subject)
		}
	}
	if values := exth[524]; len(values) > 0 {
		meta.language = decodeMOBIText(values[0], codePage)
	}
	for _, field := range []struct {
		tag  uint32
		name string
	}{{101, "publisher"}, {103, "description"}, {104, "isbn"}, {106, "published"}, {113, "asin"}} {
		if values := exth[field.tag]; len(values) > 0 {
			value := decodeMOBIText(values[0], codePage)
			switch field.name {
			case "publisher":
				meta.publisher = value
			case "description":
				meta.description = plainMOBIDescription(value)
			case "published":
				meta.published = value
			default:
				if value != "" {
					if meta.identifiers == nil {
						meta.identifiers = make(map[string]string)
					}
					meta.identifiers[field.name] = value
				}
			}
		}
	}
	if cover := mobiCoverData(f, st.Size(), offsets, header, exth); len(cover) > 0 {
		if uri, err := coverDataURI(cover); err == nil {
			meta.coverURL = uri
		}
	}
	return meta, nil
}

func parseEXTH(b []byte) (map[uint32][][]byte, error) {
	out := make(map[uint32][][]byte)
	if len(b) < 12 || string(b[:4]) != "EXTH" {
		return out, fmt.Errorf("invalid EXTH header")
	}
	total := int(binary.BigEndian.Uint32(b[4:8]))
	count := int(binary.BigEndian.Uint32(b[8:12]))
	if total < 12 || total > len(b) || count > 4096 {
		return out, fmt.Errorf("invalid EXTH length")
	}
	off := 12
	for i := 0; i < count; i++ {
		if off > total-8 {
			return nil, fmt.Errorf("truncated EXTH record")
		}
		typ := binary.BigEndian.Uint32(b[off : off+4])
		n := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		if n < 8 || n > total-off {
			return nil, fmt.Errorf("invalid EXTH record size")
		}
		out[typ] = append(out[typ], append([]byte(nil), b[off+8:off+n]...))
		off += n
	}
	return out, nil
}

func plainMOBIDescription(description string) string {
	const maxInput = 128 << 10
	const maxOutput = 32 << 10
	if len(description) > maxInput {
		description = description[:maxInput]
	}
	tokenizer := xhtml.NewTokenizer(strings.NewReader(description))
	var out strings.Builder
	hiddenDepth := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		switch tokenType {
		case xhtml.TextToken:
			if hiddenDepth == 0 && out.Len() < maxOutput {
				out.WriteString(stdhtml.UnescapeString(string(tokenizer.Text())))
				out.WriteByte(' ')
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if tokenType == xhtml.StartTagToken && (tag == "script" || tag == "style") {
				hiddenDepth++
			}
			if hiddenDepth == 0 && isDescriptionBlockTag(tag) {
				out.WriteByte('\n')
			}
		case xhtml.EndTagToken:
			tag := strings.ToLower(tokenizer.Token().Data)
			if tag == "script" || tag == "style" {
				if hiddenDepth > 0 {
					hiddenDepth--
				}
			}
			if hiddenDepth == 0 && isDescriptionBlockTag(tag) {
				out.WriteByte('\n')
			}
		}
		if out.Len() > maxOutput {
			break
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func isDescriptionBlockTag(tag string) bool {
	switch tag {
	case "address", "article", "blockquote", "br", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "ol", "p", "section", "ul":
		return true
	default:
		return false
	}
}

func mobiCoverData(f *os.File, fileSize int64, records []uint32, header []byte, exth map[uint32][][]byte) []byte {
	if len(header) < 96 || len(exth[201]) == 0 || len(exth[201][0]) < 4 {
		return nil
	}
	firstImage := binary.BigEndian.Uint32(header[92:96])
	coverOffset := binary.BigEndian.Uint32(exth[201][0][:4])
	idx := uint64(firstImage) + uint64(coverOffset)
	if firstImage == 0xffffffff || idx >= uint64(len(records)) {
		return nil
	}
	start := int64(records[idx])
	end := fileSize
	if idx+1 < uint64(len(records)) {
		end = int64(records[idx+1])
	}
	n := end - start
	if n <= 0 || n > maxCoverBytes {
		return nil
	}
	b := make([]byte, int(n))
	if _, err := f.ReadAt(b, start); err != nil && err != io.EOF {
		return nil
	}
	if _, _, ok := imageType(b); !ok {
		return nil
	}
	return b
}

func imageType(b []byte) (string, string, bool) {
	if len(b) >= 3 && bytes.Equal(b[:3], []byte{0xff, 0xd8, 0xff}) {
		return "jpeg", "image/jpeg", true
	}
	if len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "png", "image/png", true
	}
	if len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a") {
		return "gif", "image/gif", true
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "webp", "image/webp", true
	}
	return "", "", false
}

func coverDataURI(raw []byte) (string, error) {
	_, mime, ok := imageType(raw)
	if !ok {
		return "", fmt.Errorf("unsupported cover image")
	}
	if mime == "image/webp" {
		if len(raw) > 256<<10 {
			return "", fmt.Errorf("cover image too large")
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxCoverPixels {
		return "", fmt.Errorf("invalid cover dimensions")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	const maxW, maxH = 180, 270
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h), 1)
	dw, dh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := b.Min.Y + y*h/dh
		for x := 0; x < dw; x++ {
			sx := b.Min.X + x*w/dw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 78}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

func decodeMOBIText(b []byte, codePage uint32) string {
	if codePage == 65001 {
		return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
	}
	var out strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		if c >= 0x80 && c <= 0x9f {
			out.WriteRune(cp1252[c-0x80])
		} else {
			out.WriteRune(rune(c))
		}
	}
	return strings.TrimSpace(out.String())
}

var cp1252 = [32]rune{0x20ac, 0x0081, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021, 0x02c6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008d, 0x017d, 0x008f, 0x0090, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0x02dc, 0x2122, 0x0161, 0x203a, 0x0153, 0x009d, 0x017e, 0x0178}

func mobiLanguage(id uint32) string {
	langs := map[uint32]string{0x01: "ar", 0x02: "bg", 0x03: "ca", 0x04: "zh", 0x05: "cs", 0x06: "da", 0x07: "de", 0x08: "el", 0x09: "en", 0x0a: "es", 0x0b: "fi", 0x0c: "fr", 0x0d: "he", 0x0e: "hu", 0x0f: "is", 0x10: "it", 0x11: "ja", 0x12: "ko", 0x13: "nl", 0x14: "no", 0x15: "pl", 0x16: "pt", 0x17: "rm", 0x18: "ro", 0x19: "ru", 0x1a: "hr", 0x1b: "sk", 0x1c: "sq", 0x1d: "sv", 0x1e: "th", 0x1f: "tr", 0x20: "ur", 0x21: "id", 0x22: "uk", 0x23: "be", 0x24: "sl", 0x25: "et", 0x26: "lv", 0x27: "lt", 0x29: "fa", 0x2a: "vi", 0x2d: "eu", 0x2f: "mk", 0x36: "af", 0x39: "hi", 0x3e: "ms"}
	return langs[id]
}
