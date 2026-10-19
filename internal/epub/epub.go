// Package epub reads the metadata and cover of DRM-free EPUB 2 and EPUB 3 files.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const (
	maxEntries   = 10000
	maxTotalSize = 128 << 20
	maxXMLSize   = 4 << 20
	maxCoverSize = 32 << 20
	maxEntrySize = 64 << 20
)

type Metadata struct {
	Title       string
	Authors     []string
	Language    string
	Subjects    []string
	Publisher   string
	Published   string
	Identifiers map[string]string
	Description string
	Series      string
	SeriesIndex string
	AuthorSort  []string
	TitleSort   string
	PageCount   int
	Cover       []byte
	CoverExt    string
}

type dcValue struct {
	ID                string `xml:"id,attr"`
	Scheme            string `xml:"scheme,attr"`
	FileAs            string `xml:"http://www.idpf.org/2007/opf file-as,attr"`
	UnqualifiedFileAs string `xml:"file-as,attr"`
	Event             string `xml:"http://www.idpf.org/2007/opf event,attr"`
	Text              string `xml:",chardata"`
}

type containerXML struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}
type packageXML struct {
	Metadata struct {
		Title       []dcValue `xml:"http://purl.org/dc/elements/1.1/ title"`
		Creator     []dcValue `xml:"http://purl.org/dc/elements/1.1/ creator"`
		Language    []dcValue `xml:"http://purl.org/dc/elements/1.1/ language"`
		Subject     []dcValue `xml:"http://purl.org/dc/elements/1.1/ subject"`
		Publisher   []dcValue `xml:"http://purl.org/dc/elements/1.1/ publisher"`
		Date        []dcValue `xml:"http://purl.org/dc/elements/1.1/ date"`
		Identifier  []dcValue `xml:"http://purl.org/dc/elements/1.1/ identifier"`
		Description []dcValue `xml:"http://purl.org/dc/elements/1.1/ description"`
		Meta        []struct {
			ID       string `xml:"id,attr"`
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
			Refines  string `xml:"refines,attr"`
			Scheme   string `xml:"scheme,attr"`
			Text     string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Properties string `xml:"properties,attr"`
			MediaType  string `xml:"media-type,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Toc   string `xml:"toc,attr"`
		Items []struct {
			IDRef  string `xml:"idref,attr"`
			Linear string `xml:"linear,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
	Guide struct {
		Refs []struct {
			Type string `xml:"type,attr"`
			Href string `xml:"href,attr"`
		} `xml:"reference"`
	} `xml:"guide"`
}
type encryptionXML struct {
	Encrypted []struct {
		Method struct {
			Algorithm string `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		CipherData struct {
			Reference struct {
				URI string `xml:"URI,attr"`
			} `xml:"CipherReference"`
		} `xml:"CipherData"`
	} `xml:"EncryptedData"`
}

// ReplaceCover copies an EPUB while adding an Attune cover resource and making
// it the package's declared cover. The input archive is never modified.
func ReplaceCover(source, destination string, cover []byte, extension string) error {
	if extension != ".jpg" && extension != ".png" {
		return errors.New("cover must be a JPEG or PNG image")
	}
	mediaType := "image/jpeg"
	if extension == ".png" {
		mediaType = "image/png"
	}
	z, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer z.Close()
	files, err := validateArchive(z)
	if err != nil {
		return err
	}
	containerData, err := readEntry(files["META-INF/container.xml"], maxXMLSize)
	if err != nil {
		return errors.New("EPUB is missing a valid META-INF/container.xml")
	}
	var container containerXML
	if err = xml.Unmarshal(containerData, &container); err != nil || len(container.Rootfiles) == 0 {
		return errors.New("EPUB container has no package document")
	}
	opfPath, err := safeName(container.Rootfiles[0].FullPath)
	if err != nil {
		return err
	}
	if files[opfPath] == nil {
		return errors.New("EPUB package document is missing")
	}
	opfData, err := readEntry(files[opfPath], maxXMLSize)
	if err != nil {
		return err
	}
	coverID, coverHref := replacementCoverNames(opfData, extension)
	updatedOPF, err := declareReplacementCover(opfData, coverID, coverHref, mediaType)
	if err != nil {
		return err
	}
	coverName := path.Join(path.Dir(opfPath), coverHref)
	if files[coverName] != nil {
		return errors.New("EPUB already contains the Attune cover resource")
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	writeErr := error(nil)
	for _, file := range z.File {
		header := file.FileHeader
		entry, e := w.CreateHeader(&header)
		if e != nil {
			writeErr = e
			break
		}
		if file.Name == opfPath {
			_, writeErr = entry.Write(updatedOPF)
			if writeErr != nil {
				break
			}
			continue
		}
		input, e := file.Open()
		if e != nil {
			writeErr = e
			break
		}
		_, writeErr = io.Copy(entry, input)
		closeErr := input.Close()
		if writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			break
		}
	}
	if writeErr == nil {
		entry, e := w.Create(coverName)
		if e != nil {
			writeErr = e
		} else {
			_, writeErr = entry.Write(cover)
		}
	}
	if closeErr := w.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if closeErr := out.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(destination)
	}
	return writeErr
}

func replacementCoverNames(data []byte, extension string) (id, href string) {
	var pkg packageXML
	_ = xml.Unmarshal(data, &pkg)
	ids, hrefs := map[string]bool{}, map[string]bool{}
	for _, item := range pkg.Manifest.Items {
		ids[item.ID] = true
		hrefs[item.Href] = true
	}
	for n := 0; ; n++ {
		base := "attune-cover-override"
		if n > 0 {
			base += "-" + strconv.Itoa(n+1)
		}
		href = base + extension
		if !ids[base] && !hrefs[href] {
			return base, href
		}
	}
}

func declareReplacementCover(data []byte, coverID, href, mediaType string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	manifestOpen, metadataOpen := false, false
	manifestAdded, metadataAdded := false, false
	packageVersion3 := false
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse EPUB package document: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			t.Attr = stripNamespaceDeclarations(t.Attr)
			tok = t
			switch strings.ToLower(t.Name.Local) {
			case "package":
				packageVersion3 = strings.HasPrefix(strings.TrimSpace(attribute(t.Attr, "version")), "3.")
			case "manifest":
				manifestOpen = true
			case "metadata":
				metadataOpen = true
			case "item":
				if manifestOpen && tokenHas(t.Attr, "properties", "cover-image") {
					t.Attr = removePropertyToken(t.Attr, "cover-image")
					tok = t
				}
			case "meta":
				if metadataOpen && strings.EqualFold(attribute(t.Attr, "name"), "cover") {
					if err = decoder.Skip(); err != nil {
						return nil, err
					}
					continue
				}
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "manifest":
				if manifestOpen && !manifestAdded {
					attrs := []xml.Attr{{Name: xml.Name{Local: "id"}, Value: coverID}, {Name: xml.Name{Local: "href"}, Value: href}, {Name: xml.Name{Local: "media-type"}, Value: mediaType}}
					if packageVersion3 {
						attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "properties"}, Value: "cover-image"})
					}
					if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Space: t.Name.Space, Local: "item"}, Attr: attrs}); err != nil {
						return nil, err
					}
					if err := encoder.EncodeToken(xml.EndElement{Name: xml.Name{Space: t.Name.Space, Local: "item"}}); err != nil {
						return nil, err
					}
					manifestAdded = true
				}
				manifestOpen = false
			case "metadata":
				if metadataOpen && !metadataAdded {
					if err := encoder.EncodeToken(xml.StartElement{Name: xml.Name{Space: t.Name.Space, Local: "meta"}, Attr: []xml.Attr{{Name: xml.Name{Local: "name"}, Value: "cover"}, {Name: xml.Name{Local: "content"}, Value: coverID}}}); err != nil {
						return nil, err
					}
					if err := encoder.EncodeToken(xml.EndElement{Name: xml.Name{Space: t.Name.Space, Local: "meta"}}); err != nil {
						return nil, err
					}
					metadataAdded = true
				}
				metadataOpen = false
			}
		}
		if err = encoder.EncodeToken(tok); err != nil {
			return nil, err
		}
	}
	if !manifestAdded || !metadataAdded {
		return nil, errors.New("EPUB package document has no metadata or manifest")
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func attribute(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}

func stripNamespaceDeclarations(attrs []xml.Attr) []xml.Attr {
	filtered := attrs[:0]
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Space, "xmlns") || (attr.Name.Space == "" && strings.EqualFold(attr.Name.Local, "xmlns")) {
			continue
		}
		filtered = append(filtered, attr)
	}
	return filtered
}

func tokenHas(attrs []xml.Attr, name, want string) bool {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, name) && hasToken(attr.Value, want) {
			return true
		}
	}
	return false
}

func removePropertyToken(attrs []xml.Attr, property string) []xml.Attr {
	for i, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, "properties") {
			parts := strings.Fields(attr.Value)
			filtered := parts[:0]
			for _, part := range parts {
				if !strings.EqualFold(part, property) {
					filtered = append(filtered, part)
				}
			}
			if len(filtered) == 0 {
				return append(attrs[:i], attrs[i+1:]...)
			}
			attrs[i].Value = strings.Join(filtered, " ")
			return attrs
		}
	}
	return attrs
}

// Parse validates the archive structure and extracts metadata and an optional cover.
func Parse(filename string) (Metadata, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return Metadata{}, err
	}
	defer z.Close()
	files, err := validateArchive(z)
	if err != nil {
		return Metadata{}, err
	}
	containerData, err := readEntry(files["META-INF/container.xml"], maxXMLSize)
	if err != nil {
		return Metadata{}, errors.New("EPUB is missing a valid META-INF/container.xml")
	}
	var c containerXML
	if err = xml.Unmarshal(containerData, &c); err != nil || len(c.Rootfiles) == 0 {
		return Metadata{}, errors.New("EPUB container has no package document")
	}
	opfPath, err := safeName(c.Rootfiles[0].FullPath)
	if err != nil {
		return Metadata{}, err
	}
	opfData, err := readEntry(files[opfPath], maxXMLSize)
	if err != nil {
		return Metadata{}, fmt.Errorf("read package document: %w", err)
	}
	var p packageXML
	if err = xml.Unmarshal(opfData, &p); err != nil {
		return Metadata{}, fmt.Errorf("parse package document: %w", err)
	}
	base := path.Dir(opfPath)
	metadata := Metadata{}
	if len(p.Metadata.Title) > 0 {
		metadata.Title = strings.TrimSpace(p.Metadata.Title[0].Text)
	}
	for _, a := range p.Metadata.Creator {
		name := strings.TrimSpace(a.Text)
		if name != "" {
			metadata.Authors = append(metadata.Authors, name)
			sort := refinedValue(p.Metadata.Meta, a.ID, "file-as")
			if sort == "" {
				sort = strings.TrimSpace(a.FileAs)
			}
			if sort == "" {
				sort = strings.TrimSpace(a.UnqualifiedFileAs)
			}
			metadata.AuthorSort = append(metadata.AuthorSort, sort)
		}
	}
	if len(metadata.AuthorSort) > 0 {
		any := false
		for _, s := range metadata.AuthorSort {
			if s != "" {
				any = true
				break
			}
		}
		if !any {
			metadata.AuthorSort = nil
		}
	}
	if len(p.Metadata.Language) > 0 {
		metadata.Language = strings.TrimSpace(p.Metadata.Language[0].Text)
	}
	metadata.Subjects = cleanStrings(dcTexts(p.Metadata.Subject))
	if len(p.Metadata.Publisher) > 0 {
		metadata.Publisher = strings.TrimSpace(p.Metadata.Publisher[0].Text)
	}
	for _, date := range p.Metadata.Date {
		if strings.EqualFold(date.Event, "publication") {
			metadata.Published = strings.TrimSpace(date.Text)
			break
		}
	}
	if metadata.Published == "" {
		for _, date := range p.Metadata.Date {
			if strings.TrimSpace(date.Event) == "" {
				metadata.Published = strings.TrimSpace(date.Text)
				break
			}
		}
	}
	if len(p.Metadata.Description) > 0 {
		metadata.Description = plainText(p.Metadata.Description[0].Text)
	}
	metadata.Identifiers = parseIdentifiers(p.Metadata.Identifier, p.Metadata.Meta)
	if len(p.Metadata.Title) > 0 {
		metadata.TitleSort = refinedValue(p.Metadata.Meta, p.Metadata.Title[0].ID, "file-as")
		if metadata.TitleSort == "" {
			metadata.TitleSort = strings.TrimSpace(p.Metadata.Title[0].FileAs)
		}
		if metadata.TitleSort == "" {
			metadata.TitleSort = strings.TrimSpace(p.Metadata.Title[0].UnqualifiedFileAs)
		}
	}
	for _, m := range p.Metadata.Meta {
		name := strings.ToLower(strings.TrimSpace(m.Name))
		prop := strings.ToLower(strings.TrimSpace(m.Property))
		switch name {
		case "calibre:series":
			metadata.Series = strings.TrimSpace(m.Content)
		case "calibre:series_index":
			metadata.SeriesIndex = strings.TrimSpace(m.Content)
		case "calibre:title_sort":
			if metadata.TitleSort == "" {
				metadata.TitleSort = strings.TrimSpace(m.Content)
			}
		case "calibre:pages":
			metadata.PageCount = parsePageCount(m.Content)
		case "calibre:user_metadata:#pages":
			metadata.PageCount = parsePageCount(m.Content)
		}
		if prop == "page-count" && metadata.PageCount == 0 {
			metadata.PageCount = parsePageCount(m.Text)
		}
		if prop == "belongs-to-collection" && m.ID != "" {
			if strings.EqualFold(refinedValue(p.Metadata.Meta, m.ID, "collection-type"), "series") {
				metadata.Series = strings.TrimSpace(m.Text)
				metadata.SeriesIndex = refinedValue(p.Metadata.Meta, m.ID, "group-position")
			}
		}
	}
	if metadata.Title == "" {
		return Metadata{}, errors.New("EPUB has no title")
	}
	byID := make(map[string]struct {
		file  *zip.File
		media string
	}, len(p.Manifest.Items))
	var coverItem *zip.File
	var coverHref string
	for _, item := range p.Manifest.Items {
		byID[item.ID] = struct {
			file  *zip.File
			media string
		}{files[resolve(base, item.Href)], item.MediaType}
		if hasToken(item.Properties, "cover-image") {
			coverItem = files[resolve(base, item.Href)]
		}
	}
	for _, m := range p.Metadata.Meta {
		if strings.EqualFold(strings.TrimSpace(m.Name), "cover") {
			coverHref = m.Content
		}
	}
	if coverItem == nil && coverHref != "" {
		coverItem = byID[coverHref].file
	}
	if coverItem == nil {
		for _, g := range p.Guide.Refs {
			if strings.EqualFold(g.Type, "cover") {
				coverItem = files[resolve(base, strings.SplitN(g.Href, "#", 2)[0])]
			}
		}
	}
	if coverItem == nil { // A practical fallback for EPUBs with no explicit cover declaration.
		for _, it := range p.Manifest.Items {
			n := strings.ToLower(it.Href)
			if strings.Contains(n, "cover") && strings.HasPrefix(it.MediaType, "image/") {
				coverItem = files[resolve(base, it.Href)]
				break
			}
		}
	}
	if coverItem != nil {
		data, e := readEntry(coverItem, maxCoverSize)
		if e != nil {
			return Metadata{}, fmt.Errorf("read EPUB cover: %w", e)
		}
		metadata.Cover = data
		metadata.CoverExt = imageExt(coverItem.Name)
	}
	return metadata, nil
}

func dcTexts(values []dcValue) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.Text)
	}
	return out
}
func refinedValue(values []struct {
	ID       string `xml:"id,attr"`
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	Scheme   string `xml:"scheme,attr"`
	Text     string `xml:",chardata"`
}, id, property string) string {
	if id == "" {
		return ""
	}
	want := "#" + id
	for _, v := range values {
		if v.Refines == want && strings.EqualFold(v.Property, property) {
			return strings.TrimSpace(v.Text)
		}
	}
	return ""
}
func parseIdentifiers(values []dcValue, metas []struct {
	ID       string `xml:"id,attr"`
	Name     string `xml:"name,attr"`
	Content  string `xml:"content,attr"`
	Property string `xml:"property,attr"`
	Refines  string `xml:"refines,attr"`
	Scheme   string `xml:"scheme,attr"`
	Text     string `xml:",chardata"`
}) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, v := range values {
		value := strings.TrimSpace(v.Text)
		if value == "" {
			continue
		}
		scheme := strings.TrimSpace(v.Scheme)
		if scheme == "" && v.ID != "" {
			scheme = refinedValue(metas, v.ID, "identifier-type")
		}
		key := "identifier"
		lower := strings.ToLower(value)
		if scheme != "" {
			key = strings.ToUpper(scheme)
		} else if strings.HasPrefix(lower, "urn:isbn:") {
			key = "ISBN"
			value = value[len("urn:isbn:"):]
		} else if strings.HasPrefix(lower, "urn:uuid:") {
			key = "UUID"
			value = value[len("urn:uuid:"):]
		} else if strings.Contains(value, ":") {
			key = strings.ToUpper(strings.SplitN(value, ":", 2)[0])
		}
		if _, exists := out[key]; exists {
			n := 2
			for {
				candidate := fmt.Sprintf("%s#%d", key, n)
				if _, ok := out[candidate]; !ok {
					key = candidate
					break
				}
				n++
			}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func parsePageCount(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && n > 0 && n <= 100000 {
		return n
	}
	var custom map[string]any
	if json.Unmarshal([]byte(value), &custom) == nil {
		switch v := custom["#value#"].(type) {
		case float64:
			if v >= 1 && v <= 100000 && v == float64(int(v)) {
				return int(v)
			}
		case string:
			n, err = strconv.Atoi(strings.TrimSpace(v))
			if err == nil && n > 0 && n <= 100000 {
				return n
			}
		}
	}
	return 0
}
func plainText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "<") {
		return value
	}
	doc, err := html.Parse(bytes.NewBufferString(value))
	if err != nil {
		return value
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}

func validateArchive(z *zip.ReadCloser) (map[string]*zip.File, error) {
	if len(z.File) == 0 || len(z.File) > maxEntries {
		return nil, errors.New("EPUB has an invalid number of entries")
	}
	files := make(map[string]*zip.File, len(z.File))
	var total uint64
	for _, f := range z.File {
		clean, err := safeName(f.Name)
		if err != nil {
			return nil, err
		}
		if f.Flags&1 != 0 {
			return nil, fmt.Errorf("encrypted EPUB entry: %s", f.Name)
		}
		if f.Mode()&0o170000 == 0o120000 {
			return nil, errors.New("EPUB contains a symbolic link")
		}
		total += f.UncompressedSize64
		if f.UncompressedSize64 > maxEntrySize || total > maxTotalSize {
			return nil, errors.New("EPUB exceeds extraction size limits")
		}
		if _, exists := files[clean]; exists {
			return nil, fmt.Errorf("duplicate EPUB path %q", clean)
		}
		files[clean] = f
	}
	if enc := files["META-INF/encryption.xml"]; enc != nil {
		data, e := readEntry(enc, maxXMLSize)
		if e != nil {
			return nil, errors.New("invalid EPUB encryption metadata")
		}
		var encryption encryptionXML
		if e = xml.Unmarshal(data, &encryption); e != nil {
			return nil, errors.New("invalid EPUB encryption metadata")
		}
		for _, item := range encryption.Encrypted {
			switch item.Method.Algorithm {
			case "http://www.idpf.org/2008/embedding", "http://ns.adobe.com/pdf/enc#RC":
				// These algorithms are permitted only for actual font resources.
				uri := strings.SplitN(strings.SplitN(item.CipherData.Reference.URI, "#", 2)[0], "?", 2)[0]
				name, nameErr := safeName(uri)
				if nameErr != nil || !fontFile(name) {
					return nil, errors.New("EPUB font obfuscation references a non-font resource")
				}
				if files[name] == nil {
					return nil, errors.New("EPUB font obfuscation references a missing resource")
				}
			default:
				return nil, fmt.Errorf("unsupported EPUB encryption algorithm %q", item.Method.Algorithm)
			}
		}
	}
	return files, nil
}
func fontFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".otf", ".ttf", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

func safeName(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe EPUB path %q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe EPUB path %q", name)
	}
	return clean, nil
}
func resolve(base, href string) string {
	// Hrefs may carry a fragment or query; those are not part of ZIP member names.
	href = strings.SplitN(strings.SplitN(href, "#", 2)[0], "?", 2)[0]
	name, err := safeName(path.Join(base, href))
	if err != nil {
		return ""
	}
	return name
}
func readEntry(f *zip.File, limit uint64) ([]byte, error) {
	if f == nil {
		return nil, errors.New("archive entry not found")
	}
	if f.UncompressedSize64 > limit {
		return nil, errors.New("archive entry exceeds size limit")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > limit {
		return nil, errors.New("archive entry exceeds size limit")
	}
	return data, nil
}
func hasToken(s, token string) bool {
	for _, x := range strings.Fields(s) {
		if x == token {
			return true
		}
	}
	return false
}
func imageExt(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png":
		return ".png"
	case ".gif":
		return ".gif"
	case ".webp":
		return ".webp"
	}
	return ".img"
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[strings.ToLower(value)] {
			out = append(out, value)
			seen[strings.ToLower(value)] = true
		}
	}
	return out
}
