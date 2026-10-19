package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclareReplacementCoverKeepsOPFNamespacesAndAttributes(t *testing.T) {
	input := []byte(`<package xmlns="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf" version="3.0"><metadata><dc:title id="title">Title</dc:title><dc:creator opf:file-as="Writer, A">Writer</dc:creator><meta name="cover" content="old"/></metadata><manifest><item id="old" href="old.jpg" media-type="image/jpeg" properties="cover-image"/></manifest></package>`)
	output, err := declareReplacementCover(input, "attune-cover-override", "cover.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte("_xmlns:")) {
		t.Fatalf("serialized XML contains an invalid namespace attribute: %s", output)
	}
	root := bytes.SplitN(output, []byte(">"), 2)[0]
	if bytes.Count(root, []byte(`xmlns="http://www.idpf.org/2007/opf"`)) != 1 {
		t.Fatalf("root has duplicate/default namespace declarations: %s", root)
	}
	decoder := xml.NewDecoder(bytes.NewReader(output))
	var sawTitle, sawCreator, sawCover bool
	for {
		token, decodeErr := decoder.Token()
		if decodeErr != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok {
			switch start.Name.Local {
			case "title":
				sawTitle = start.Name.Space == "http://purl.org/dc/elements/1.1/" && attribute(start.Attr, "id") == "title"
			case "creator":
				for _, attr := range start.Attr {
					if attr.Name.Local == "file-as" && attr.Name.Space == "http://www.idpf.org/2007/opf" && attr.Value == "Writer, A" {
						sawCreator = true
					}
				}
			case "item":
				sawCover = attribute(start.Attr, "id") == "attune-cover-override" && attribute(start.Attr, "properties") == "cover-image"
			}
		}
	}
	if !sawTitle || !sawCreator || !sawCover {
		t.Fatalf("lost namespace or metadata while rewriting: title=%v creator=%v cover=%v; %s", sawTitle, sawCreator, sawCover, output)
	}
}

func TestDeclareReplacementCoverUsesEPUB2CoverMetadata(t *testing.T) {
	input := []byte(`<package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Title</dc:title></metadata><manifest/><spine/></package>`)
	output, err := declareReplacementCover(input, "attune-cover-override", "cover.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(output))
	var foundCoverMeta, foundImage bool
	for {
		token, decodeErr := decoder.Token()
		if decodeErr != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local == "meta" && attribute(start.Attr, "name") == "cover" && attribute(start.Attr, "content") == "attune-cover-override" {
				foundCoverMeta = true
			}
			if start.Name.Local == "item" && attribute(start.Attr, "id") == "attune-cover-override" {
				foundImage = attribute(start.Attr, "media-type") == "image/jpeg" && attribute(start.Attr, "properties") == ""
			}
		}
	}
	if !foundCoverMeta || !foundImage {
		t.Fatalf("EPUB2 cover declaration is incorrect: meta=%v item=%v; %s", foundCoverMeta, foundImage, output)
	}
}

func TestReplaceCoverAddsCoverWhenEPUBHasNone(t *testing.T) {
	source := filepath.Join(t.TempDir(), "no-cover.epub")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>No cover</dc:title></metadata><manifest/><spine/></package>`,
	} {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	cover := []byte("cover bytes")
	output := filepath.Join(t.TempDir(), "with-cover.epub")
	if err = ReplaceCover(source, output, cover, ".png"); err != nil {
		t.Fatal(err)
	}
	metadata, err := Parse(output)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CoverExt != ".png" || !bytes.Equal(metadata.Cover, cover) {
		t.Fatalf("cover = %q %q", metadata.CoverExt, metadata.Cover)
	}
}

func writeEncryptedFixture(t *testing.T, algorithm, uri string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "encrypted.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml":  `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
		"OPS/book.opf":            `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Title</dc:title></metadata><manifest/><spine/></package>`,
		"META-INF/encryption.xml": `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><EncryptedData><EncryptionMethod Algorithm="` + algorithm + `"/><CipherData><CipherReference URI="` + uri + `"/></CipherData></EncryptedData></encryption>`,
		uri:                       "obfuscated font bytes",
	}
	for name, body := range entries {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestEncryptionAllowsFontObfuscationAndRejectsEncryptedContent(t *testing.T) {
	fontAlgorithm := "http://www.idpf.org/2008/embedding"
	contentAlgorithm := "http://www.w3.org/2001/04/xmlenc#aes256-cbc"
	if _, err := Parse(writeEncryptedFixture(t, fontAlgorithm, "OPS/fonts/book.ttf")); err != nil {
		t.Fatalf("font obfuscation rejected: %v", err)
	}
	for _, tc := range []struct{ algorithm, uri string }{
		{contentAlgorithm, "OPS/chapter.xhtml"},
		{fontAlgorithm, "OPS/chapter.xhtml"},
		{fontAlgorithm, "../outside.ttf"},
	} {
		if _, err := Parse(writeEncryptedFixture(t, tc.algorithm, tc.uri)); err == nil {
			t.Errorf("accepted encrypted/non-font resource %q with algorithm %q", tc.uri, tc.algorithm)
		}
	}
}

func TestParseEPUB2MetadataAndLegacyCoverDeclaration(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
		"OPS/book.opf":           `<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title opf:file-as="Legacy, Title">Legacy Title</dc:title><dc:creator opf:file-as="Author, First">First Author</dc:creator><dc:creator>Second Author</dc:creator><dc:date opf:event="modification">2024-05-01</dc:date><dc:date opf:event="publication">2020-03</dc:date><dc:language>de</dc:language><meta name="cover" content="cover-image"/></metadata><manifest><item id="cover-image" href="cover.jpg" media-type="image/jpeg"/></manifest><spine/></package>`,
		"OPS/cover.jpg":          "legacy-cover",
	}
	for name, body := range entries {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	meta, err := Parse(filename)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Legacy Title" || meta.TitleSort != "Legacy, Title" || meta.Published != "2020-03" || meta.PageCount != 0 || len(meta.AuthorSort) != 2 || meta.AuthorSort[0] != "Author, First" || meta.AuthorSort[1] != "" || meta.Language != "de" || len(meta.Authors) != 2 || meta.Authors[0] != "First Author" || meta.CoverExt != ".jpg" || string(meta.Cover) != "legacy-cover" {
		t.Fatalf("unexpected EPUB2 metadata: %#v", meta)
	}
}

func TestParseRichEPUB3MetadataOnlyWhenEmbedded(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "rich.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
		"OPS/book.opf":           `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title id="title">Echo Man</dc:title><dc:creator id="author">Sam Holland</dc:creator><dc:publisher>Crooked Lane Books</dc:publisher><dc:date>2022-04</dc:date><dc:identifier id="isbn" scheme="ISBN">9781643859910</dc:identifier><dc:description>&lt;p&gt;A plain description.&lt;/p&gt;</dc:description><meta refines="#title" property="file-as">Echo Man, The</meta><meta refines="#author" property="file-as">Holland, Sam</meta><meta id="series" property="belongs-to-collection">Mystery Series</meta><meta refines="#series" property="collection-type">series</meta><meta refines="#series" property="group-position">2.5</meta><meta name="calibre:user_metadata:#pages" content="{&quot;#value#&quot;:309}"/></metadata><manifest/><spine/></package>`,
	}
	for name, body := range entries {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	meta, err := Parse(filename)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Publisher != "Crooked Lane Books" || meta.Published != "2022-04" || meta.Description != "A plain description." || meta.Series != "Mystery Series" || meta.SeriesIndex != "2.5" || meta.TitleSort != "Echo Man, The" || len(meta.AuthorSort) != 1 || meta.AuthorSort[0] != "Holland, Sam" || meta.PageCount != 309 || meta.Identifiers["ISBN"] != "9781643859910" {
		t.Fatalf("rich metadata: %#v", meta)
	}
}

func TestPreviewSanitizesMarkupAndLocalResources(t *testing.T) {
	filename := writePreviewFixture(t)
	chapters, err := Preview(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 || chapters[0].ID != "one" || chapters[0].Title != "Navigation First" || chapters[1].Title != "NCX Second" {
		t.Fatalf("spine chapters = %#v", chapters)
	}
	content, mt, err := Resource(filename, "OPS/Text/one.xhtml", "book-id")
	if err != nil {
		t.Fatal(err)
	}
	if mt != "text/html; charset=utf-8" {
		t.Fatalf("markup content type %q", mt)
	}
	text := string(content)
	for _, forbidden := range []string{"<script", "onclick", "evil.test", "bad.test", "<animate", "javascript:", "http-equiv=\"refresh\"", "src=\"//"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Errorf("unsafe markup retained %q in %s", forbidden, text)
		}
	}
	for _, required := range []string{"/preview/book-id/OPS/Images/p.png", "/preview/book-id/OPS/Styles/book.css", "href=\"#part\"", "/preview/book-id/OPS/Text/two.xhtml"} {
		if !strings.Contains(text, required) {
			t.Errorf("expected safe/local markup %q in %s", required, text)
		}
	}
	css, _, err := Resource(filename, "OPS/Styles/book.css", "book-id")
	if err != nil {
		t.Fatal(err)
	}
	cssText := string(css)
	for _, forbidden := range []string{"remote.test", "@import url(https"} {
		if strings.Contains(cssText, forbidden) {
			t.Errorf("remote CSS retained %q in %s", forbidden, cssText)
		}
	}
	if !strings.Contains(cssText, "url(../Images/p.png)") || !strings.Contains(cssText, "@import \"chapter.css\"") {
		t.Errorf("local CSS resource/import not preserved: %s", cssText)
	}
	if _, _, err = Resource(filename, "../outside.txt", "book-id"); err == nil {
		t.Fatal("traversal resource accepted")
	}
}

func writePreviewFixture(t *testing.T) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "preview.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Preview</dc:title><dc:language>en</dc:language><dc:subject>Fiction</dc:subject><dc:subject>Mystery</dc:subject></metadata><manifest><item id="one" href="Text/one.xhtml" media-type="application/xhtml+xml"/><item id="two" href="Text/two.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/><item id="css" href="Styles/book.css" media-type="text/css"/><item id="img" href="Images/p.png" media-type="image/png"/></manifest><spine toc="ncx"><itemref idref="one"/><itemref idref="two"/></spine></package>`,
		"OPS/nav.xhtml":          `<html xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="Text/one.xhtml">Navigation First</a></li></ol></nav></body></html>`,
		"OPS/toc.ncx":            `<ncx><navMap><navPoint><navLabel><text>NCX Second</text></navLabel><content src="Text/two.xhtml#part"/></navPoint></navMap></ncx>`,
		"OPS/Text/one.xhtml":     `<html><head><title>First Chapter</title><meta http-equiv="refresh" content="0;url=https://evil.test"><link rel="stylesheet" href="../Styles/book.css"><style>@import url(https://evil.test/x.css);p{background:url(https://evil.test/x.png)}</style><script>alert(1)</script></head><body><h1 onclick="evil()">First</h1><img src="../Images/p.png" onerror="evil()"><img srcset="https://evil.test/p.png 1x, ../Images/p.png 2x"><svg><animate attributeName="href" to="https://evil.test"/><a xlink:href="https://evil.test">bad</a></svg><a href="#part">part</a><a href="https://evil.test/redirect">external</a><a href="two.xhtml">next</a></body></html>`,
		"OPS/Text/two.xhtml":     `<html><head><title>Second Chapter</title></head><body><h1>Two</h1></body></html>`,
		"OPS/Styles/book.css":    `@import "chapter.css"; @import url(https://remote.test/evil.css); body{background:url(../Images/p.png)} .x{background:url(https://remote.test/image.png)}`,
		"OPS/Styles/chapter.css": `p{color:red}`,
		"OPS/Images/p.png":       "image",
	}
	for name, body := range entries {
		e, er := w.Create(name)
		if er != nil {
			t.Fatal(er)
		}
		if _, er = e.Write([]byte(body)); er != nil {
			t.Fatal(er)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestPreviewRejectsArchiveTraversal(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "traversal.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("escape"))
	_ = w.Close()
	_ = f.Close()
	if _, err = Preview(filename); err == nil {
		t.Fatal("archive traversal accepted")
	}
}

func TestPreviewPrefersHeadingsAndFallsBackForRepeatedDocumentTitles(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "headings.epub")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Book Name</dc:title></metadata><manifest><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/><item id="two" href="two.xhtml" media-type="application/xhtml+xml"/><item id="three" href="three.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/><itemref idref="two"/><itemref idref="three"/></spine></package>`,
		"OPS/one.xhtml":          `<html><head><title>Book Name</title></head><body><h2>Distinct Chapter</h2></body></html>`,
		"OPS/two.xhtml":          `<html><head><title>Book Name</title></head><body><p>No heading</p></body></html>`,
		"OPS/three.xhtml":        `<html><head><title>Book Name</title></head><body><p>No heading</p></body></html>`,
	}
	for name, body := range entries {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	chapters, err := Preview(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 3 || chapters[0].Title != "Distinct Chapter" || chapters[1].Title != "Book Name" || chapters[2].Title != "Chapter 3" {
		t.Fatalf("chapter titles = %#v", chapters)
	}
}
