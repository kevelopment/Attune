package device

import (
	"reflect"
	"testing"
)

func TestResolveProfileFromDeviceDescriptor(t *testing.T) {
	tests := []struct {
		name       string
		descriptor DeviceDescriptor
		wantID     string
	}{
		{"kindle label", DeviceDescriptor{Label: "Kindle"}, "kindle"},
		{"amazon vendor", DeviceDescriptor{Vendor: "Amazon", Product: "e-reader"}, "kindle"},
		{"kobo product", DeviceDescriptor{Vendor: "Rakuten", Product: "Kobo Clara"}, "kobo"},
		{"pocketbook name", DeviceDescriptor{Name: "PocketBook Era"}, "pocketbook"},
		{"tolino name", DeviceDescriptor{Name: "tolino vision"}, "tolino"},
		{"nook product", DeviceDescriptor{Product: "NOOK GlowLight"}, "nook"},
		{"boox vendor", DeviceDescriptor{Vendor: "ONYX", Product: "BOOX"}, "boox-android"},
		{"android name stays generic", DeviceDescriptor{Name: "Android eReader"}, "generic"},
		{"supernote label", DeviceDescriptor{Label: "Supernote"}, "supernote"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ResolveProfile(test.descriptor)
			if got.ID != test.wantID {
				t.Fatalf("ResolveProfile(%+v).ID = %q, want %q", test.descriptor, got.ID, test.wantID)
			}
		})
	}
}

func TestUnknownDeviceUsesExplicitGenericFallback(t *testing.T) {
	got := ResolveProfile(DeviceDescriptor{Label: "Reader Disk", Vendor: "Example", Product: "Model 42"})
	if got.ID != "generic" || !got.RequiresExplicitSelection {
		t.Fatalf("unknown device profile = %+v, want explicit generic fallback", got)
	}
	if len(got.TransferFormats) != 0 {
		t.Fatalf("generic profile must not guess supported formats: %v", got.TransferFormats)
	}
}

func TestProfileTransferFormatPreferencesAreOrdered(t *testing.T) {
	kindle := ResolveProfile(DeviceDescriptor{Product: "Kindle"})
	want := []string{"azw3", "mobi", "prc"}
	if !reflect.DeepEqual(kindle.TransferFormats, want) {
		t.Fatalf("Kindle transfer formats = %v, want %v", kindle.TransferFormats, want)
	}
	if !kindle.AllowEPUBToMOBI {
		t.Fatal("Kindle profile must retain the existing EPUB-to-MOBI path")
	}

	kobo := ResolveProfile(DeviceDescriptor{Product: "Kobo"})
	if !reflect.DeepEqual(kobo.TransferFormats, []string{"epub", "pdf", "cbz"}) {
		t.Fatalf("Kobo transfer formats = %v", kobo.TransferFormats)
	}
	if kobo.AllowEPUBToMOBI {
		t.Fatal("non-Kindle profile unexpectedly enables EPUB-to-MOBI conversion")
	}
}

func TestResolvedProfilesDoNotExposeMutableRegistrySlices(t *testing.T) {
	first := ResolveProfile(DeviceDescriptor{Product: "Kobo"})
	first.TransferFormats[0] = "changed"
	first.BookFolders[0] = "changed"
	second := ResolveProfile(DeviceDescriptor{Product: "Kobo"})
	if second.TransferFormats[0] != "epub" || second.BookFolders[0] != "Books" {
		t.Fatalf("mutating a resolved profile changed defaults: %+v", second)
	}
}
