package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:alpha|beta|rc)(?:\.(0|[1-9][0-9]*))?))?$`)

type Version struct {
	Major, Minor, Patch uint64
	Pre                 string
}

func ParseVersion(raw string) (Version, error) {
	m := versionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return Version{}, fmt.Errorf("unsupported version %q", raw)
	}
	major, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("version component overflows: %w", err)
	}
	minor, err := strconv.ParseUint(m[2], 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("version component overflows: %w", err)
	}
	patch, err := strconv.ParseUint(m[3], 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("version component overflows: %w", err)
	}
	return Version{Major: major, Minor: minor, Patch: patch, Pre: m[4]}, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

func (v Version) IsPrerelease() bool { return v.Pre != "" }

func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	if v.Pre == other.Pre {
		return 0
	}
	if v.Pre == "" {
		return 1
	}
	if other.Pre == "" {
		return -1
	}
	parts := func(s string) (string, uint64, bool) {
		p := strings.SplitN(s, ".", 2)
		if len(p) == 1 {
			return p[0], 0, false
		}
		n, _ := strconv.ParseUint(p[1], 10, 64)
		return p[0], n, true
	}
	pa, na, hasA := parts(v.Pre)
	pb, nb, hasB := parts(other.Pre)
	order := map[string]int{"alpha": 0, "beta": 1, "rc": 2}
	if order[pa] < order[pb] {
		return -1
	}
	if order[pa] > order[pb] {
		return 1
	}
	if hasA != hasB {
		if !hasA {
			return -1
		}
		return 1
	}
	if na < nb {
		return -1
	}
	if na > nb {
		return 1
	}
	return 0
}
