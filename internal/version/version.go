// Package version provides semantic version parsing and bump logic.
//
// It supports the full semver 2.0.0 format including pre-release and build
// metadata, and defines a BumpKind type whose values are ordered so the
// highest required bump can be tracked with a simple > comparison.
//
// Basic usage:
//
//	v, err := version.Parse("1.2.3")
//	next := v.Bump(version.BumpMinor) // → 1.3.0
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// BumpKind represents the type of semver bump to apply.
// Values are ordered so that > yields the higher-priority bump.
type BumpKind int

const (
	// BumpNone indicates no version change is required.
	BumpNone BumpKind = iota
	// BumpPatch indicates a backwards-compatible bug fix.
	BumpPatch
	// BumpMinor indicates a backwards-compatible new feature.
	BumpMinor
	// BumpMajor indicates a breaking change.
	BumpMajor
)

// String returns the lower-case name of the bump kind.
func (b BumpKind) String() string {
	switch b {
	case BumpPatch:
		return "patch"
	case BumpMinor:
		return "minor"
	case BumpMajor:
		return "major"
	default:
		return "none"
	}
}

// Semver represents a semantic version as defined by semver.org.
// Pre-release and build metadata are stored as raw strings.
type Semver struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
	Build      string
}

// Parse parses a semantic version string, accepting an optional "v" or "V" prefix.
// It returns an error if the string is not a valid semver.
func Parse(s string) (Semver, error) {
	if s == "" {
		return Semver{}, fmt.Errorf("empty version string")
	}

	// Strip optional v/V prefix.
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")

	if s == "" {
		return Semver{}, fmt.Errorf("version string is only a prefix")
	}

	var build, preRelease string

	// Split off build metadata at first '+'.
	if idx := strings.Index(s, "+"); idx >= 0 {
		build = s[idx+1:]
		s = s[:idx]
	}

	// Split off pre-release at first '-'.
	if idx := strings.Index(s, "-"); idx >= 0 {
		preRelease = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("version %q must have exactly three dot-separated parts", s)
	}

	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" {
			return Semver{}, fmt.Errorf("version part %d is empty", i)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("version part %q is not a non-negative integer", p)
		}
		nums[i] = n
	}

	return Semver{
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		PreRelease: preRelease,
		Build:      build,
	}, nil
}

// String formats the version as "MAJOR.MINOR.PATCH", appending "-PreRelease"
// and "+Build" when those fields are non-empty. No "v" prefix is included.
func (s Semver) String() string {
	core := fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
	if s.PreRelease != "" {
		core += "-" + s.PreRelease
	}
	if s.Build != "" {
		core += "+" + s.Build
	}
	return core
}

// IsInitialDevelopment reports whether the version is in initial development
// (Major == 0), during which semver stability guarantees are relaxed.
func (s Semver) IsInitialDevelopment() bool {
	return s.Major == 0
}

// Bump returns a new Semver with the given bump applied. Pre-release and build
// metadata are always dropped on a non-zero bump. BumpNone returns a copy of
// the receiver unchanged.
func (s Semver) Bump(kind BumpKind) Semver {
	switch kind {
	case BumpMajor:
		return Semver{Major: s.Major + 1}
	case BumpMinor:
		return Semver{Major: s.Major, Minor: s.Minor + 1}
	case BumpPatch:
		return Semver{Major: s.Major, Minor: s.Minor, Patch: s.Patch + 1}
	default:
		return s
	}
}
