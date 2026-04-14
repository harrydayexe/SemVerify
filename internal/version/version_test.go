package version_test

import (
	"testing"

	"github.com/harrydayexe/SemVerify/internal/version"
)

func TestParse_Valid(t *testing.T) {
	tests := []struct {
		input      string
		wantMajor  int
		wantMinor  int
		wantPatch  int
		wantPre    string
		wantBuild  string
	}{
		{"1.2.3", 1, 2, 3, "", ""},
		{"v1.2.3", 1, 2, 3, "", ""},
		{"V1.2.3", 1, 2, 3, "", ""},
		{"0.1.0", 0, 1, 0, "", ""},
		{"0.0.0", 0, 0, 0, "", ""},
		{"100.200.300", 100, 200, 300, "", ""},
		{"1.0.0-alpha", 1, 0, 0, "alpha", ""},
		{"1.0.0-alpha-1", 1, 0, 0, "alpha-1", ""},
		{"1.0.0+build.123", 1, 0, 0, "", "build.123"},
		{"1.0.0-alpha+build.123", 1, 0, 0, "alpha", "build.123"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := version.Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.input, err)
			}
			if got.Major != tc.wantMajor || got.Minor != tc.wantMinor || got.Patch != tc.wantPatch {
				t.Errorf("Parse(%q) = %d.%d.%d, want %d.%d.%d",
					tc.input, got.Major, got.Minor, got.Patch,
					tc.wantMajor, tc.wantMinor, tc.wantPatch)
			}
			if got.PreRelease != tc.wantPre {
				t.Errorf("Parse(%q) PreRelease = %q, want %q", tc.input, got.PreRelease, tc.wantPre)
			}
			if got.Build != tc.wantBuild {
				t.Errorf("Parse(%q) Build = %q, want %q", tc.input, got.Build, tc.wantBuild)
			}
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	cases := []string{
		"",
		"v",
		"1.2",
		"abc",
		"1.2.3.4",
		"1.2.a",
		"-1.2.3",
		"1..3",
		"1.2.",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := version.Parse(input)
			if err == nil {
				t.Errorf("Parse(%q) expected error, got nil", input)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"1.0.0-alpha", "1.0.0-alpha"},
		{"1.0.0+build.123", "1.0.0+build.123"},
		{"1.0.0-alpha+build.123", "1.0.0-alpha+build.123"},
		{"1.0.0-alpha-1", "1.0.0-alpha-1"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			v, err := version.Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if got := v.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBump(t *testing.T) {
	base := version.Semver{Major: 1, Minor: 2, Patch: 3}

	tests := []struct {
		name string
		v    version.Semver
		kind version.BumpKind
		want string
	}{
		{"patch", base, version.BumpPatch, "1.2.4"},
		{"minor", base, version.BumpMinor, "1.3.0"},
		{"major", base, version.BumpMajor, "2.0.0"},
		{"none", base, version.BumpNone, "1.2.3"},
		{"pre-release dropped on patch", version.Semver{Major: 1, Minor: 0, Patch: 0, PreRelease: "alpha"}, version.BumpPatch, "1.0.1"},
		{"pre-release dropped on minor", version.Semver{Major: 1, Minor: 0, Patch: 0, PreRelease: "alpha"}, version.BumpMinor, "1.1.0"},
		{"pre-release dropped on major", version.Semver{Major: 1, Minor: 0, Patch: 0, PreRelease: "alpha"}, version.BumpMajor, "2.0.0"},
		{"none preserves pre-release", version.Semver{Major: 1, Minor: 0, Patch: 0, PreRelease: "alpha", Build: "001"}, version.BumpNone, "1.0.0-alpha+001"},
		{"zero major patch", version.Semver{Major: 0, Minor: 1, Patch: 0}, version.BumpPatch, "0.1.1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.v.Bump(tc.kind)
			if got.String() != tc.want {
				t.Errorf("Bump(%v) = %q, want %q", tc.kind, got.String(), tc.want)
			}
		})
	}
}

func TestIsInitialDevelopment(t *testing.T) {
	tests := []struct {
		v    version.Semver
		want bool
	}{
		{version.Semver{Major: 0, Minor: 0, Patch: 0}, true},
		{version.Semver{Major: 0, Minor: 1, Patch: 0}, true},
		{version.Semver{Major: 0, Minor: 99, Patch: 99}, true},
		{version.Semver{Major: 1, Minor: 0, Patch: 0}, false},
		{version.Semver{Major: 2, Minor: 3, Patch: 4}, false},
	}
	for _, tc := range tests {
		if got := tc.v.IsInitialDevelopment(); got != tc.want {
			t.Errorf("%v.IsInitialDevelopment() = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestBumpKind_String(t *testing.T) {
	tests := []struct {
		k    version.BumpKind
		want string
	}{
		{version.BumpNone, "none"},
		{version.BumpPatch, "patch"},
		{version.BumpMinor, "minor"},
		{version.BumpMajor, "major"},
	}
	for _, tc := range tests {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("BumpKind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestBumpKind_Ordering(t *testing.T) {
	if !(version.BumpMajor > version.BumpMinor) {
		t.Error("BumpMajor should be > BumpMinor")
	}
	if !(version.BumpMinor > version.BumpPatch) {
		t.Error("BumpMinor should be > BumpPatch")
	}
	if !(version.BumpPatch > version.BumpNone) {
		t.Error("BumpPatch should be > BumpNone")
	}
}
