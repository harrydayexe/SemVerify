// Command semverify automates semantic versioning by tracking a Go module's
// public API surface at the source level.
//
// Instead of parsing commit messages, semverify uses go/ast to extract every
// exported symbol, saves a snapshot manifest to disk, and diffs snapshots to
// determine the correct semver bump.
//
// Usage:
//
//	semverify init [--version 0.1.0] [--track-tags] [--track-values]
//	semverify diff [--json]
//	semverify check --proposed <version>
//	semverify snapshot [--version <v>] [--stdout]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/harrydayexe/SemVerify/internal/differ"
	"github.com/harrydayexe/SemVerify/internal/extractor"
	"github.com/harrydayexe/SemVerify/internal/snapshot"
	"github.com/harrydayexe/SemVerify/internal/version"
)

func main() {
	cmd := &cli.Command{
		Name:    "semverify",
		Usage:   "Automated semantic versioning via Go API surface tracking",
		Version: "0.1.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Value: ".",
				Usage: "path to the Go module root",
			},
			&cli.StringFlag{
				Name:  "snapshot-file",
				Value: ".semver-snapshot.json",
				Usage: "path to the snapshot file (relative to --dir unless absolute)",
			},
		},
		Commands: []*cli.Command{
			initCmd(),
			snapshotCmd(),
			diffCmd(),
			checkCmd(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// resolveSnapshotPath returns the absolute path to the snapshot file.
// If the snapshot-file flag is relative, it is resolved against the dir flag.
func resolveSnapshotPath(cmd *cli.Command) string {
	snapFile := cmd.String("snapshot-file")
	if filepath.IsAbs(snapFile) {
		return snapFile
	}
	return filepath.Join(cmd.String("dir"), snapFile)
}

// initCmd returns the `semverify init` command, which performs first-time setup
// by extracting the API surface and writing the initial snapshot.
func initCmd() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Extract the API surface and write the initial snapshot",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "version",
				Value: "0.1.0",
				Usage: "initial version to record in the snapshot",
			},
			&cli.BoolFlag{
				Name:  "track-tags",
				Usage: "enable struct field tag tracking (tag changes = breaking)",
			},
			&cli.BoolFlag{
				Name:  "track-values",
				Usage: "enable const value tracking (value changes = breaking)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ver := cmd.String("version")
			if _, err := version.Parse(ver); err != nil {
				return fmt.Errorf("invalid version %q: %w", ver, err)
			}

			dir := cmd.String("dir")
			snap, err := extractor.Extract(extractor.ExtractOptions{
				ModuleDir:        dir,
				TrackFieldTags:   cmd.Bool("track-tags"),
				TrackConstValues: cmd.Bool("track-values"),
			})
			if err != nil {
				return fmt.Errorf("extracting API surface: %w", err)
			}

			snap.Version = ver
			snap.CreatedAt = time.Now().UTC().Format(time.RFC3339)

			snapPath := resolveSnapshotPath(cmd)
			if err := snap.Save(snapPath); err != nil {
				return fmt.Errorf("saving snapshot: %w", err)
			}

			symbolCount := countSymbols(snap)
			fmt.Printf("Initialized snapshot\n")
			fmt.Printf("  Module:   %s\n", snap.Module)
			fmt.Printf("  Version:  %s\n", snap.Version)
			fmt.Printf("  Packages: %d\n", len(snap.Packages))
			fmt.Printf("  Symbols:  %d\n", symbolCount)
			fmt.Printf("  File:     %s\n", snapPath)
			return nil
		},
	}
}

// snapshotCmd returns the `semverify snapshot` command, which re-captures the
// API surface to update the baseline after a release.
func snapshotCmd() *cli.Command {
	return &cli.Command{
		Name:  "snapshot",
		Usage: "Re-capture the API surface (use after a release to update the baseline)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "version",
				Usage: "version to tag the new snapshot with (defaults to existing version)",
			},
			&cli.BoolFlag{
				Name:  "stdout",
				Usage: "print the snapshot JSON to stdout instead of writing to file",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			snapPath := resolveSnapshotPath(cmd)

			old, err := snapshot.Load(snapPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("snapshot not found at %q — run 'semverify init' first", snapPath)
				}
				return fmt.Errorf("loading snapshot: %w", err)
			}

			snap, err := extractor.Extract(extractor.ExtractOptions{
				ModuleDir:        cmd.String("dir"),
				TrackFieldTags:   old.Options.TrackFieldTags,
				TrackConstValues: old.Options.TrackConstValues,
			})
			if err != nil {
				return fmt.Errorf("extracting API surface: %w", err)
			}

			// Preserve version unless overridden.
			snap.Version = old.Version
			if v := cmd.String("version"); v != "" {
				if _, err := version.Parse(v); err != nil {
					return fmt.Errorf("invalid version %q: %w", v, err)
				}
				snap.Version = v
			}

			if cmd.Bool("stdout") {
				data, err := json.MarshalIndent(snap, "", "  ")
				if err != nil {
					return fmt.Errorf("serializing snapshot: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			if err := snap.Save(snapPath); err != nil {
				return fmt.Errorf("saving snapshot: %w", err)
			}
			fmt.Printf("Snapshot updated: %s (version %s)\n", snapPath, snap.Version)
			return nil
		},
	}
}

// diffCmd returns the `semverify diff` command, which compares the live source
// against the committed snapshot and reports changes.
func diffCmd() *cli.Command {
	return &cli.Command{
		Name:  "diff",
		Usage: "Compare live source against the committed snapshot",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "json",
				Usage: "output the diff result as JSON",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			snapPath := resolveSnapshotPath(cmd)

			old, err := snapshot.Load(snapPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("snapshot not found at %q — run 'semverify init' first", snapPath)
				}
				return fmt.Errorf("loading snapshot: %w", err)
			}

			current, err := extractor.Extract(extractor.ExtractOptions{
				ModuleDir:        cmd.String("dir"),
				TrackFieldTags:   old.Options.TrackFieldTags,
				TrackConstValues: old.Options.TrackConstValues,
			})
			if err != nil {
				return fmt.Errorf("extracting API surface: %w", err)
			}
			current.Version = old.Version

			result := differ.Diff(old, current)

			if cmd.Bool("json") {
				data, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return fmt.Errorf("serializing result: %w", err)
				}
				fmt.Println(string(data))
				return nil
			}

			printDiffResult(result, old.Version)
			return nil
		},
	}
}

// checkCmd returns the `semverify check` command, a CI gate that validates a
// proposed version string against the actual API changes.
func checkCmd() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Validate a proposed version against the actual API changes (CI gate)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "proposed",
				Usage:    "the version you intend to release",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			snapPath := resolveSnapshotPath(cmd)

			old, err := snapshot.Load(snapPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("snapshot not found at %q — run 'semverify init' first", snapPath)
				}
				return fmt.Errorf("loading snapshot: %w", err)
			}

			current, err := extractor.Extract(extractor.ExtractOptions{
				ModuleDir:        cmd.String("dir"),
				TrackFieldTags:   old.Options.TrackFieldTags,
				TrackConstValues: old.Options.TrackConstValues,
			})
			if err != nil {
				return fmt.Errorf("extracting API surface: %w", err)
			}
			current.Version = old.Version

			result := differ.Diff(old, current)

			proposedStr := cmd.String("proposed")
			proposed, err := version.Parse(proposedStr)
			if err != nil {
				return fmt.Errorf("invalid proposed version %q: %w", proposedStr, err)
			}

			oldVer, err := version.Parse(old.Version)
			if err != nil {
				return fmt.Errorf("invalid snapshot version %q: %w", old.Version, err)
			}

			// During initial development (0.x.y), semver rules are relaxed.
			if oldVer.IsInitialDevelopment() {
				fmt.Printf("INFO: Module is in initial development (v%s). Semver rules are relaxed.\n", old.Version)
				fmt.Printf("  Detected bump: %s\n", result.MaxBump)
				fmt.Printf("  Proposed:      %s\n", proposedStr)
				fmt.Println("PASS")
				return nil
			}

			// For v1.0.0+, validate that the proposed version is sufficient.
			sufficient := isVersionSufficient(oldVer, proposed, result.MaxBump)
			if !sufficient {
				minRequired := oldVer.Bump(result.MaxBump).String()
				return fmt.Errorf(
					"version %q is insufficient for the detected changes\n  Required bump: %s (minimum: %s)\n  Proposed:      %s",
					proposedStr, result.MaxBump, minRequired, proposedStr,
				)
			}

			fmt.Printf("PASS: %s is a valid %s bump from %s\n", proposedStr, result.MaxBump, old.Version)
			return nil
		},
	}
}

// isVersionSufficient reports whether proposed is at least as large as the
// minimum version required by maxBump applied to oldVer.
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
	default: // BumpNone: any version >= old is fine
		return proposed.Major > oldVer.Major ||
			(proposed.Major == oldVer.Major && proposed.Minor > oldVer.Minor) ||
			(proposed.Major == oldVer.Major && proposed.Minor == oldVer.Minor && proposed.Patch >= oldVer.Patch)
	}
}

// printDiffResult prints a human-readable diff result to stdout.
func printDiffResult(result differ.Result, oldVersion string) {
	if len(result.Changes) == 0 {
		fmt.Printf("No API changes detected (current: %s)\n", oldVersion)
		fmt.Printf("Recommended: patch bump → %s\n", result.NextVersion)
		return
	}

	fmt.Printf("API changes detected (%d total):\n\n", len(result.Changes))

	for _, c := range result.Changes {
		icon := changeIcon(c.Kind)
		fmt.Printf("  %s [%s] %s\n", icon, c.Bump, c.Description)
	}

	fmt.Printf("\nRecommended bump: %s\n", result.MaxBump)
	fmt.Printf("Current version:  %s\n", oldVersion)
	fmt.Printf("Next version:     %s\n", result.NextVersion)
}

// changeIcon returns a single-character icon for a change kind.
func changeIcon(kind differ.ChangeKind) string {
	switch kind {
	case differ.ChangeAdded:
		return "+"
	case differ.ChangeRemoved:
		return "-"
	case differ.ChangeDeprecated:
		return "!"
	default:
		return "~"
	}
}

// countSymbols returns the total number of exported symbols across all packages.
func countSymbols(snap *snapshot.Snapshot) int {
	total := 0
	for _, pkg := range snap.Packages {
		total += len(pkg.Funcs) + len(pkg.Types) + len(pkg.Interfaces) + len(pkg.Consts) + len(pkg.Vars)
	}
	return total
}
