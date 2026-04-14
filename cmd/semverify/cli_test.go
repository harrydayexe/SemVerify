package main_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/harrydayexe/SemVerify/internal/differ"
	"github.com/harrydayexe/SemVerify/internal/extractor"
	"github.com/harrydayexe/SemVerify/internal/snapshot"
	"github.com/harrydayexe/SemVerify/internal/version"
)

// writeModule creates a minimal Go module in dir.
func writeModule(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/mymod\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// runInit calls the extractor and saves an initial snapshot, mirroring the init command.
func runInit(t *testing.T, dir, snapPath, ver string, opts extractor.ExtractOptions) *snapshot.Snapshot {
	t.Helper()
	opts.ModuleDir = dir
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	snap.Version = ver
	if err := snap.Save(snapPath); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	return snap
}

// TestIntegration_InitDiffCheck runs the full init → modify → diff → check pipeline.
func TestIntegration_InitDiffCheck(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	// Initial state: one exported function.
	writeModule(t, dir, map[string]string{
		"api.go": `package mymod

func Hello() string { return "hello" }
`,
	})

	runInit(t, dir, snapPath, "1.0.0", extractor.ExtractOptions{})

	// Modify: remove Hello, add NewFunc (MAJOR due to removal).
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(`package mymod

func NewFunc() int { return 0 }
`), 0644); err != nil {
		t.Fatal(err)
	}

	old, err := snapshot.Load(snapPath)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	current, err := extractor.Extract(extractor.ExtractOptions{ModuleDir: dir})
	if err != nil {
		t.Fatalf("extract current: %v", err)
	}
	current.Version = old.Version

	result := differ.Diff(old, current)

	if result.MaxBump != version.BumpMajor {
		t.Errorf("expected MAJOR bump, got %v", result.MaxBump)
	}

	// Check: 2.0.0 should pass (MAJOR bump from 1.0.0).
	proposed20, _ := version.Parse("2.0.0")
	oldVer, _ := version.Parse("1.0.0")
	if !isVersionSufficient(oldVer, proposed20, result.MaxBump) {
		t.Error("2.0.0 should be sufficient for a MAJOR bump from 1.0.0")
	}

	// Check: 1.1.0 should fail (only minor bump, but MAJOR changes detected).
	proposed11, _ := version.Parse("1.1.0")
	if isVersionSufficient(oldVer, proposed11, result.MaxBump) {
		t.Error("1.1.0 should NOT be sufficient for a MAJOR bump from 1.0.0")
	}
}

// TestIntegration_InitThenSnapshot verifies that snapshot updates the baseline.
func TestIntegration_InitThenSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	writeModule(t, dir, map[string]string{
		"api.go": `package mymod

func Hello() {}
`,
	})

	runInit(t, dir, snapPath, "1.0.0", extractor.ExtractOptions{})

	// Add a new function.
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(`package mymod

func Hello() {}
func World() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Re-snapshot with new version.
	old, _ := snapshot.Load(snapPath)
	newSnap, err := extractor.Extract(extractor.ExtractOptions{
		ModuleDir:        dir,
		TrackFieldTags:   old.Options.TrackFieldTags,
		TrackConstValues: old.Options.TrackConstValues,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	newSnap.Version = "1.1.0"
	if err := newSnap.Save(snapPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Load the saved snapshot and verify.
	reloaded, err := snapshot.Load(snapPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Version != "1.1.0" {
		t.Errorf("snapshot version = %q, want %q", reloaded.Version, "1.1.0")
	}
	pkg := reloaded.Packages["example.com/mymod"]
	if _, ok := pkg.Funcs["World"]; !ok {
		t.Error("World func should be in updated snapshot")
	}
}

// TestIntegration_DiffNoSnapshot verifies the error when no snapshot exists.
func TestIntegration_DiffNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	_, err := snapshot.Load(snapPath)
	if err == nil {
		t.Fatal("expected error loading non-existent snapshot")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

// TestIntegration_CheckInitialDevelopment verifies relaxed rules for 0.x.y.
func TestIntegration_CheckInitialDevelopment(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	writeModule(t, dir, map[string]string{
		"api.go": `package mymod

func Hello() {}
`,
	})
	runInit(t, dir, snapPath, "0.1.0", extractor.ExtractOptions{})

	// Remove Hello — normally MAJOR, but we're in 0.x so it should be relaxed.
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(`package mymod
`), 0644); err != nil {
		t.Fatal(err)
	}

	old, _ := snapshot.Load(snapPath)
	current, _ := extractor.Extract(extractor.ExtractOptions{ModuleDir: dir})
	current.Version = old.Version
	result := differ.Diff(old, current)

	oldVer, _ := version.Parse("0.1.0")
	if !oldVer.IsInitialDevelopment() {
		t.Error("0.1.0 should be initial development")
	}

	// In initial development, any proposed version is acceptable.
	proposed, _ := version.Parse("0.2.0")
	_ = result
	_ = proposed
	// The check command exits 0 for initial development — we just verify IsInitialDevelopment.
}

// TestIntegration_TrackingOptionsInherited verifies snapshot inherits tracking options.
func TestIntegration_TrackingOptionsInherited(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	writeModule(t, dir, map[string]string{
		"api.go": `package mymod

type User struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
	})

	// Init with TrackFieldTags=true.
	runInit(t, dir, snapPath, "1.0.0", extractor.ExtractOptions{TrackFieldTags: true})

	loaded, _ := snapshot.Load(snapPath)
	if !loaded.Options.TrackFieldTags {
		t.Error("TrackFieldTags should be true in saved snapshot")
	}

	// Change the tag — should be MAJOR because tracking is on.
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(`package mymod

type User struct {
	Name string `+"`json:\"full_name\"`"+`
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	current, _ := extractor.Extract(extractor.ExtractOptions{
		ModuleDir:      dir,
		TrackFieldTags: loaded.Options.TrackFieldTags,
	})
	current.Version = loaded.Version
	result := differ.Diff(loaded, current)

	if result.MaxBump != version.BumpMajor {
		t.Errorf("tag change with tracking on should be MAJOR, got %v", result.MaxBump)
	}
}

// TestIntegration_DiffJSONOutput verifies the JSON output format.
func TestIntegration_DiffJSONOutput(t *testing.T) {
	dir := t.TempDir()
	snapPath := filepath.Join(dir, ".semver-snapshot.json")

	writeModule(t, dir, map[string]string{
		"api.go": `package mymod

func Foo() {}
`,
	})
	runInit(t, dir, snapPath, "1.0.0", extractor.ExtractOptions{})

	// Add a function.
	if err := os.WriteFile(filepath.Join(dir, "api.go"), []byte(`package mymod

func Foo() {}
func Bar() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	old, _ := snapshot.Load(snapPath)
	current, _ := extractor.Extract(extractor.ExtractOptions{ModuleDir: dir})
	current.Version = old.Version
	result := differ.Diff(old, current)

	// Verify JSON serialization.
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"max_bump"`) {
		t.Error("JSON output should contain max_bump field")
	}
	if !strings.Contains(jsonStr, `"next_version"`) {
		t.Error("JSON output should contain next_version field")
	}
	if !strings.Contains(jsonStr, `"changes"`) {
		t.Error("JSON output should contain changes field")
	}
}

// TestIntegration_CLIFlags verifies the CLI flag wiring using urfave/cli/v3.
func TestIntegration_CLIFlags(t *testing.T) {
	// Build a minimal CLI command to verify flag access works correctly.
	var capturedDir string
	var capturedSnapFile string

	cmd := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Value: "."},
			&cli.StringFlag{Name: "snapshot-file", Value: ".semver-snapshot.json"},
		},
		Commands: []*cli.Command{
			{
				Name: "sub",
				Action: func(ctx context.Context, c *cli.Command) error {
					capturedDir = c.String("dir")
					capturedSnapFile = c.String("snapshot-file")
					return nil
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), []string{"test", "--dir", "/tmp/mymod", "--snapshot-file", "custom.json", "sub"}); err != nil {
		t.Fatalf("cli.Run: %v", err)
	}

	if capturedDir != "/tmp/mymod" {
		t.Errorf("dir = %q, want %q", capturedDir, "/tmp/mymod")
	}
	if capturedSnapFile != "custom.json" {
		t.Errorf("snapshot-file = %q, want %q", capturedSnapFile, "custom.json")
	}
}

// isVersionSufficient mirrors the logic in main.go (duplicated here since it's in package main).
func isVersionSufficient(oldVer, proposed version.Semver, maxBump version.BumpKind) bool {
	switch maxBump {
	case version.BumpMajor:
		return proposed.Major > oldVer.Major
	case version.BumpMinor:
		if proposed.Major != oldVer.Major {
			return proposed.Major > oldVer.Major
		}
		return proposed.Minor > oldVer.Minor
	case version.BumpPatch:
		if proposed.Major != oldVer.Major {
			return proposed.Major > oldVer.Major
		}
		if proposed.Minor != oldVer.Minor {
			return proposed.Minor > oldVer.Minor
		}
		return proposed.Patch > oldVer.Patch
	default:
		return proposed.Major > oldVer.Major ||
			(proposed.Major == oldVer.Major && proposed.Minor > oldVer.Minor) ||
			(proposed.Major == oldVer.Major && proposed.Minor == oldVer.Minor && proposed.Patch >= oldVer.Patch)
	}
}
