package updater

import "testing"

func TestVersionOrdering(t *testing.T) {
	cases := []struct{ lower, higher string }{
		{"1.0.0-alpha", "1.0.0-alpha.0"},
		{"1.0.0-alpha.2", "1.0.0-alpha.10"},
		{"1.0.0-alpha.10", "1.0.0-beta"},
		{"1.0.0-beta.2", "1.0.0-rc.1"},
		{"1.0.0-rc.9", "1.0.0"},
		{"1.9.9", "2.0.0-alpha"},
	}
	for _, tc := range cases {
		lower, err := ParseVersion(tc.lower)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.lower, err)
		}
		higher, err := ParseVersion(tc.higher)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.higher, err)
		}
		if got := lower.Compare(higher); got >= 0 {
			t.Errorf("%s.Compare(%s) = %d, want < 0", tc.lower, tc.higher, got)
		}
		if got := higher.Compare(lower); got <= 0 {
			t.Errorf("%s.Compare(%s) = %d, want > 0", tc.higher, tc.lower, got)
		}
	}
}

func TestParseVersionRejectsMalformedAndOverflow(t *testing.T) {
	for _, raw := range []string{"", "1.2", "01.2.3", "1.2.3-", "1.2.3-gamma", "18446744073709551616.0.0", "1.18446744073709551616.0", "1.0.18446744073709551616"} {
		if _, err := ParseVersion(raw); err == nil {
			t.Errorf("ParseVersion(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestVersionStringAndPrerelease(t *testing.T) {
	v, err := ParseVersion("v2.3.4-rc.2")
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "2.3.4-rc.2" || !v.IsPrerelease() {
		t.Fatalf("parsed version = %+v", v)
	}
	stable, err := ParseVersion("2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if stable.IsPrerelease() {
		t.Fatal("stable version marked prerelease")
	}
}
