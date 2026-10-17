package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type Chapter struct{ ID, Title, Href string }

type ncxPoint struct {
	Label struct {
		Text string `xml:"text"`
	} `xml:"navLabel"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxPoint `xml:"navPoint"`
}
type ncxDocument struct {
	Points []ncxPoint `xml:"navMap>navPoint"`
}

// Preview returns readable XHTML chapters in spine order, using archive-relative hrefs.
func Preview(filename string) ([]Chapter, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	files, err := validateArchive(z)
	if err != nil {
		return nil, err
	}
	var c containerXML
	data, err := readEntry(files["META-INF/container.xml"], maxXMLSize)
	if err != nil {
		return nil, err
	}
	if err = xml.Unmarshal(data, &c); err != nil || len(c.Rootfiles) == 0 {
		return nil, errors.New("EPUB has no package document")
	}
	opf, err := safeName(c.Rootfiles[0].FullPath)
	if err != nil {
		return nil, err
	}
	data, err = readEntry(files[opf], maxXMLSize)
	if err != nil {
		return nil, err
	}
	var p packageXML
	if err = xml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	base := path.Dir(opf)
	type item struct{ href, media string }
	manifest := make(map[string]item, len(p.Manifest.Items))
	for _, it := range p.Manifest.Items {
		resolved := resolve(base, it.Href)
		if resolved == "" || files[resolved] == nil {
			continue
		}
		manifest[it.ID] = item{resolved, it.MediaType}
	}
	navigation := make(map[string]string)
	for _, entry := range p.Manifest.Items {
		if hasToken(entry.Properties, "nav") && entry.MediaType == "application/xhtml+xml" {
			if nav := files[resolve(base, entry.Href)]; nav != nil {
				collectNavHTML(nav, navigation)
			}
		}
	}
	if toc, ok := manifest[p.Spine.Toc]; ok && toc.media == "application/x-dtbncx+xml" {
		if file := files[toc.href]; file != nil {
			collectNCX(file, navigation)
		}
	}
	chapters := make([]Chapter, 0, len(p.Spine.Items))
	seenTitles := make(map[string]bool)
	for _, ref := range p.Spine.Items {
		it, ok := manifest[ref.IDRef]
		if !ok || (it.media != "application/xhtml+xml" && it.media != "text/html") {
			continue
		}
		chapterTitle := strings.TrimSpace(navigation[it.href])
		if chapterTitle == "" {
			chapterTitle = strings.TrimSpace(documentTitle(files[it.href]))
		}
		key := strings.ToLower(strings.Join(strings.Fields(chapterTitle), " "))
		if chapterTitle == "" || seenTitles[key] {
			chapterTitle = fmt.Sprintf("Chapter %d", len(chapters)+1)
			key = strings.ToLower(chapterTitle)
		}
		seenTitles[key] = true
		chapters = append(chapters, Chapter{ID: ref.IDRef, Title: chapterTitle, Href: it.href})
	}
	return chapters, nil
}

func collectNavHTML(f *zip.File, titles map[string]string) {
	b, err := readEntry(f, maxXMLSize)
	if err != nil {
		return
	}
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return
	}
	base := path.Dir(f.Name)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := attr(n, "href")
			title := strings.TrimSpace(textContent(n))
			if href != "" && title != "" {
				if target := resolve(base, href); target != "" {
					if _, exists := titles[target]; !exists {
						titles[target] = title
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

func collectNCX(f *zip.File, titles map[string]string) {
	b, err := readEntry(f, maxXMLSize)
	if err != nil {
		return
	}
	var doc ncxDocument
	if xml.Unmarshal(b, &doc) != nil {
		return
	}
	base := path.Dir(f.Name)
	var visit func([]ncxPoint)
	visit = func(points []ncxPoint) {
		for _, point := range points {
			target := resolve(base, point.Content.Src)
			label := strings.TrimSpace(point.Label.Text)
			if target != "" && label != "" {
				if _, exists := titles[target]; !exists {
					titles[target] = label
				}
			}
			visit(point.Children)
		}
	}
	visit(doc.Points)
}

func documentTitle(f *zip.File) string {
	b, err := readEntry(f, maxXMLSize)
	if err != nil {
		return ""
	}
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "h1" || n.Data == "h2" || n.Data == "h3") {
			title = strings.TrimSpace(textContent(n))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if title != "" {
		return title
	}
	var titleNode func(*html.Node)
	titleNode = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" {
			title = strings.TrimSpace(textContent(n))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			titleNode(c)
		}
	}
	titleNode(doc)
	return title
}
func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

var cssImport = regexp.MustCompile(`(?is)@import\s+(?:url\()?\s*(['"]?)([^'"\s);]+)['"]?\s*\)?\s*[^;]*;?`)
var cssURL = regexp.MustCompile(`(?is)url\(\s*(['"]?)([^)'"\s]+)['"]?\s*\)`)

// Resource reads a bounded archive entry and sanitizes markup and styles before serving it.
func Resource(filename, name, bookID string) ([]byte, string, error) {
	name, err := safeName(name)
	if err != nil {
		return nil, "", err
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, "", err
	}
	defer z.Close()
	files, err := validateArchive(z)
	if err != nil {
		return nil, "", err
	}
	f := files[name]
	if f == nil {
		return nil, "", errors.New("EPUB resource not found")
	}
	b, err := readEntry(f, maxEntrySize)
	if err != nil {
		return nil, "", err
	}
	mt := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if mt == "" {
		mt = "application/octet-stream"
	}
	if strings.Contains(mt, "html") || strings.HasSuffix(strings.ToLower(name), ".xhtml") || strings.HasSuffix(strings.ToLower(name), ".htm") {
		b, err = sanitizeHTML(b, bookID, name)
		mt = "text/html; charset=utf-8"
	} else if strings.HasSuffix(strings.ToLower(name), ".css") {
		b = []byte(sanitizeCSS(string(b)))
	}
	return b, mt, err
}

func sanitizeHTML(b []byte, id, resource string) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	base := path.Dir(resource)
	var clean func(*html.Node)
	clean = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && strings.Contains("|script|iframe|frame|frameset|object|embed|form|base|portal|set|animate|animatemotion|animatetransform|", "|"+strings.ToLower(c.Data)+"|") {
				n.RemoveChild(c)
				c = next
				continue
			}
			if c.Type == html.ElementNode && c.Data == "meta" && strings.EqualFold(attr(c, "http-equiv"), "refresh") {
				n.RemoveChild(c)
				c = next
				continue
			}
			for i := 0; i < len(c.Attr); {
				a := &c.Attr[i]
				key := strings.ToLower(a.Key)
				if strings.HasPrefix(key, "on") || key == "srcdoc" || key == "action" || key == "formaction" {
					c.Attr = append(c.Attr[:i], c.Attr[i+1:]...)
					continue
				}
				if key == "style" {
					a.Val = sanitizeCSS(a.Val)
				}
				if key == "srcset" {
					a.Val = sanitizeSrcset(a.Val, base, id)
					if a.Val == "" {
						c.Attr = append(c.Attr[:i], c.Attr[i+1:]...)
						continue
					}
				}
				if key == "src" || key == "href" || key == "poster" || key == "xlink:href" {
					v := strings.TrimSpace(a.Val)
					low := strings.ToLower(v)
					if key == "href" && strings.HasPrefix(v, "#") {
						i++
						continue
					}
					if strings.HasPrefix(low, "data:image/png") || strings.HasPrefix(low, "data:image/jpeg") || strings.HasPrefix(low, "data:image/gif") {
						i++
						continue
					}
					if strings.Contains(low, ":") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "#") {
						c.Attr = append(c.Attr[:i], c.Attr[i+1:]...)
						continue
					}
					parts := strings.SplitN(v, "#", 2)
					local, e := url.PathUnescape(parts[0])
					target := ""
					if e == nil {
						target, e = safeName(path.Join(base, local))
					}
					if e != nil || target == "" {
						c.Attr = append(c.Attr[:i], c.Attr[i+1:]...)
						continue
					}
					mapped := "/preview/" + id + "/" + escapePath(target)
					if len(parts) == 2 {
						mapped += "#" + parts[1]
					}
					a.Val = mapped
				}
				i++
			}
			if c.Type == html.ElementNode && c.Data == "style" {
				for child := c.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode {
						child.Data = sanitizeCSS(child.Data)
					}
				}
			}
			clean(c)
			c = next
		}
	}
	clean(doc)
	var out bytes.Buffer
	if err = html.Render(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func escapePath(name string) string {
	parts := strings.Split(name, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
func sanitizeCSS(s string) string {
	s = cssImport.ReplaceAllStringFunc(s, func(match string) string {
		m := cssImport.FindStringSubmatch(match)
		if len(m) < 3 {
			return ""
		}
		v := strings.ToLower(m[2])
		if strings.Contains(v, ":") || strings.HasPrefix(m[2], "//") || strings.HasPrefix(v, "data:") {
			return ""
		}
		return match
	})
	return cssURL.ReplaceAllStringFunc(s, func(match string) string {
		m := cssURL.FindStringSubmatch(match)
		if len(m) < 3 {
			return ""
		}
		v := strings.TrimSpace(m[2])
		low := strings.ToLower(v)
		if strings.HasPrefix(low, "http:") || strings.HasPrefix(low, "https:") || strings.HasPrefix(v, "//") || strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "data:") {
			return "url()"
		}
		return match
	})
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}
func sanitizeSrcset(input, base, id string) string {
	var out []string
	for _, candidate := range strings.Split(input, ",") {
		fields := strings.Fields(candidate)
		if len(fields) == 0 {
			continue
		}
		raw := fields[0]
		low := strings.ToLower(raw)
		if strings.Contains(low, ":") || strings.HasPrefix(raw, "//") || strings.HasPrefix(low, "data:") {
			continue
		}
		decoded, e := url.PathUnescape(raw)
		if e != nil {
			continue
		}
		target, e := safeName(path.Join(base, decoded))
		if e != nil || target == "" {
			continue
		}
		mapped := "/preview/" + id + "/" + escapePath(target)
		if len(fields) > 1 {
			mapped += " " + fields[1]
		}
		out = append(out, mapped)
	}
	return strings.Join(out, ", ")
}
