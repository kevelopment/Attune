package calibre

import (
	"encoding/json"
	"testing"
)

func TestParseAnnotationsKeepsValidCFIAndMalformedItemsUnplaced(t *testing.T) {
	fixture := json.RawMessage(`{
		"last_read_positions":[{"format":"EPUB","cfi":"epubcfi(/6/2!/4/2/2/1:0)","pos_frac":0.75}],
		"annotations":[
			{"annot_id":"highlight-1","annot_type":"highlight","annot_data":"{\"cfi\":\"epubcfi(/6/4!/4/2/2:3)\",\"text\":\"Margin quote\"}","searchable_text":"A highlighted line"},
			{"annot_id":"bad-1","annot_type":"note","annot_data":"{\"cfi\":\"epubcfi(/6//4)\",\"note\":\"Keep this\"}"}
		]
	}`)
	items, err := ParseAnnotations(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("parsed %d items, want 3: %#v", len(items), items)
	}
	if items[0].Kind != "position" || items[0].CFI != "epubcfi(/6/2!/4/2/2/1:0)" || items[0].Progress == nil || *items[0].Progress != .75 {
		t.Fatalf("position was not retained: %#v", items[0])
	}
	if items[0].Format != "epub" {
		t.Fatalf("format was not normalized: %#v", items[0])
	}
	if items[1].CFI != "epubcfi(/6/4!/4/2/2:3)" || items[1].Quote != "A highlighted line" || items[1].Status != "unplaced" {
		t.Fatalf("valid highlight CFI or quote was lost: %#v", items[1])
	}
	if items[2].CFI != "" || items[2].Status != "unplaced" || items[2].Note != "Keep this" {
		t.Fatalf("malformed note did not remain unplaced: %#v", items[2])
	}
}

func TestParseAnnotationsDoesNotTreatPDFLocationAsEPUBCFI(t *testing.T) {
	items, err := ParseAnnotations(json.RawMessage(`{"annotations":[{"format":"PDF","annot_id":"pdf-note","annot_type":"note","annot_data":"{\"cfi\":\"epubcfi(/6/4)\",\"note\":\"not an EPUB position\"}"}]}`))
	if err != nil || len(items) != 1 {
		t.Fatalf("ParseAnnotations = %#v, %v", items, err)
	}
	if items[0].Format != "pdf" || items[0].CFI != "" || items[0].Status != "unplaced" {
		t.Fatalf("PDF CFI was incorrectly treated as EPUB location: %#v", items[0])
	}
}

func TestParseAnnotationsLeavesPercentageOnlyPositionUnplaced(t *testing.T) {
	items, err := ParseAnnotations(json.RawMessage(`{"last_read_positions":[{"format":"EPUB","pos_frac":0.42}]}`))
	if err != nil || len(items) != 1 {
		t.Fatalf("ParseAnnotations = %#v, %v", items, err)
	}
	if items[0].CFI != "" || items[0].Status != "unplaced" || items[0].Progress == nil || *items[0].Progress != .42 {
		t.Fatalf("percentage was used as a placement: %#v", items[0])
	}
}

func TestValidEPUBCFIRejectsMalformedAnchors(t *testing.T) {
	for _, cfi := range []string{"epubcfi(/6/4)", "epubcfi(/6/4!/4/2:3[chapter])", "epubcfi(/6/4,/1:3,/1:8)"} {
		if !ValidEPUBCFI(cfi) {
			t.Errorf("ValidEPUBCFI(%q) = false", cfi)
		}
	}
	for _, cfi := range []string{"", "epubcfi()", "epubcfi(/6//4)", "epubcfi(/6/4", "epubcfi(/6/[4])", "epubcfi(/6/4[broken)"} {
		if ValidEPUBCFI(cfi) {
			t.Errorf("ValidEPUBCFI(%q) = true", cfi)
		}
	}
}
