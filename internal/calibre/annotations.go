package calibre

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Annotation is a normalized Calibre reading-state item. CFI is retained only
// when it is structurally valid; Progress is informational and never locates
// an annotation.
type Annotation struct {
	SourceID  string   `json:"sourceId"`
	Format    string   `json:"format,omitempty"`
	Kind      string   `json:"kind"`
	Text      string   `json:"text,omitempty"`
	Note      string   `json:"note,omitempty"`
	Quote     string   `json:"quote,omitempty"`
	CFI       string   `json:"cfi,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
	Progress  *float64 `json:"progress,omitempty"`
	Status    string   `json:"status"`
}

// ParseAnnotations extracts positions and annotations from the raw JSON
// payload returned by ReadLibrary. Malformed individual rows are preserved as
// unplaced records whenever their table row can still be decoded.
func ParseAnnotations(raw json.RawMessage) ([]Annotation, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var state map[string][]map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode Calibre reading state: %w", err)
	}
	items := make([]Annotation, 0)
	for _, row := range state["last_read_positions"] {
		item := normalizeCalibreRow(row, "position")
		if value, ok := number(row["pos_frac"]); ok && value >= 0 && value <= 1 {
			item.Progress = &value
		}
		items = append(items, item)
	}
	for _, row := range state["annotations"] {
		kind := strings.ToLower(strings.TrimSpace(stringValue(row["annot_type"])))
		if kind == "" {
			kind = "annotation"
		}
		item := normalizeCalibreRow(row, kind)
		items = append(items, item)
	}
	return items, nil
}

func normalizeCalibreRow(row map[string]any, kind string) Annotation {
	item := Annotation{Kind: kind, Format: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(firstString(row, "format"))), "."), Status: "unplaced"}
	item.SourceID = firstString(row, "annot_id", "id")
	item.Timestamp = firstString(row, "timestamp", "epoch", "created")
	if item.SourceID == "" {
		encoded, _ := json.Marshal(row)
		sum := sha256.Sum256(encoded)
		item.SourceID = "row:" + hex.EncodeToString(sum[:])
	}
	if timestamp, ok := number(row["timestamp"]); ok && timestamp > 0 {
		item.Timestamp = time.UnixMilli(int64(timestamp * 1000)).UTC().Format(time.RFC3339Nano)
	} else if timestamp, ok := number(row["epoch"]); ok && timestamp > 0 {
		item.Timestamp = time.UnixMilli(int64(timestamp * 1000)).UTC().Format(time.RFC3339Nano)
	}
	item.Text = firstString(row, "note", "text", "comment")
	item.Note = firstString(row, "note", "comment")
	item.Quote = firstString(row, "searchable_text", "quote", "selected_text")
	data := row["annot_data"]
	if encoded, ok := data.(string); ok {
		var parsed map[string]any
		if json.Unmarshal([]byte(encoded), &parsed) != nil {
			// Some older Calibre databases and export fixtures persist the JSON
			// object with an extra layer of quote escaping.
			encoded = strings.ReplaceAll(encoded, `\"`, `"`)
		}
		if json.Unmarshal([]byte(encoded), &parsed) == nil {
			data = parsed
		}
	}
	if parsed, ok := data.(map[string]any); ok {
		if item.Text == "" {
			item.Text = firstString(parsed, "note", "text", "comment", "content")
		}
		if item.Note == "" {
			item.Note = firstString(parsed, "note", "comment", "annotation")
		}
		if item.Quote == "" {
			item.Quote = firstString(parsed, "quote", "selected_text", "highlight", "text")
		}
		item.CFI = firstString(parsed, "cfi", "epubcfi", "start_cfi")
	}
	if item.CFI == "" {
		item.CFI = firstString(row, "cfi", "epubcfi", "start_cfi")
	}
	if item.CFI != "" && item.Format != "" && item.Format != "epub" {
		item.CFI = ""
	}
	if item.CFI != "" && ValidEPUBCFI(item.CFI) {
		item.Status = "unplaced" // Placement is confirmed by the active EPUB reader.
	} else {
		item.CFI = ""
	}
	return item
}

// ValidEPUBCFI rejects malformed strings before they reach a reader engine.
// It validates the EPUB CFI envelope and component structure while allowing
// assertions and range endpoints used by real Calibre exports.
func ValidEPUBCFI(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "epubcfi(") || !strings.HasSuffix(value, ")") || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "epubcfi("), ")")
	if inner == "" || !strings.HasPrefix(inner, "/") {
		return false
	}
	brackets := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '[':
			if brackets != 0 {
				return false
			}
			brackets = 1
		case ']':
			if brackets == 0 {
				return false
			}
			brackets = 0
		case '(', ')':
			return false
		}
	}
	if brackets != 0 {
		return false
	}
	// Every navigation/content path must contain a numbered step. Range CFI
	// endpoints are separated by commas and can independently start with /.
	for _, part := range strings.FieldsFunc(inner, func(r rune) bool { return r == '!' || r == ',' }) {
		if !strings.HasPrefix(part, "/") || len(part) < 2 {
			return false
		}
		digit := false
		for i := 1; i < len(part); i++ {
			if part[i] >= '0' && part[i] <= '9' {
				digit = true
			}
			if part[i] == '/' && (i+1 == len(part) || part[i+1] < '0' || part[i+1] > '9') {
				return false
			}
		}
		if !digit {
			return false
		}
	}
	return true
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(row[key])); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	switch x := value.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}

func number(value any) (float64, bool) {
	switch x := value.(type) {
	case float64:
		return x, true
	case json.Number:
		v, err := x.Float64()
		return v, err == nil
	case string:
		v, err := strconv.ParseFloat(x, 64)
		return v, err == nil
	default:
		return 0, false
	}
}
