// Package snapshot provides the core data model for semverify API surface snapshots.
//
// A Snapshot captures all exported symbols of a Go module at a point in time.
// Snapshots are serialized to JSON and committed to version control, forming
// the baseline for subsequent diff operations.
//
// Basic usage:
//
//	snap := &snapshot.Snapshot{Module: "github.com/example/mymod"}
//	if err := snap.Save(".semver-snapshot.json"); err != nil {
//	    log.Fatal(err)
//	}
//	loaded, err := snapshot.Load(".semver-snapshot.json")
package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Options records which opt-in tracking flags were active when the snapshot
// was created. These are persisted so that diff and check commands inherit them.
type Options struct {
	// TrackFieldTags enables capturing and diffing struct field tags.
	// A tag change is treated as a breaking change.
	TrackFieldTags bool `json:"track_field_tags"`
	// TrackConstValues enables capturing and diffing exported constant values.
	// A value change is treated as a breaking change.
	TrackConstValues bool `json:"track_const_values"`
}

// Param represents a single function parameter or return value.
type Param struct {
	// Name is the parameter name, if present. May be empty for unnamed parameters.
	Name string `json:"name,omitempty"`
	// Type is the Go type string for this parameter.
	Type string `json:"type"`
}

// Func captures the signature of an exported package-level function.
type Func struct {
	// Params is the ordered list of parameters.
	Params []Param `json:"params,omitempty"`
	// Returns is the ordered list of return values.
	Returns []Param `json:"returns,omitempty"`
	// Variadic is true if the last parameter is variadic (...T).
	Variadic bool `json:"variadic,omitempty"`
}

// Method captures the signature of an exported method on a type.
type Method struct {
	Func
	// Receiver is the receiver type string (e.g. "*MyType" or "MyType").
	Receiver string `json:"receiver"`
}

// Field captures an exported struct field.
type Field struct {
	// Type is the Go type string for this field.
	Type string `json:"type"`
	// Tags holds the parsed struct tag values, keyed by tag name (e.g. "json", "xml").
	// Only populated when TrackFieldTags is enabled.
	Tags map[string]string `json:"tags,omitempty"`
}

// Value captures an exported constant or variable.
type Value struct {
	// Type is the Go type string. May be empty for untyped constants.
	Type string `json:"type,omitempty"`
	// Value holds the literal value. Only populated for consts when TrackConstValues is enabled.
	Value string `json:"value,omitempty"`
	// Deprecated is true when the symbol's doc comment contains "Deprecated:".
	Deprecated bool `json:"deprecated,omitempty"`
}

// Type captures an exported named type, struct, or type alias.
type Type struct {
	// Kind is one of "struct", "named", or "alias".
	Kind string `json:"kind"`
	// Underlying is the underlying type string for named types and aliases.
	Underlying string `json:"underlying,omitempty"`
	// Fields holds the exported fields for struct types.
	Fields map[string]Field `json:"fields,omitempty"`
	// Methods holds the exported methods defined on this type.
	Methods map[string]Method `json:"methods,omitempty"`
	// Embedded lists the names of embedded types.
	Embedded []string `json:"embedded,omitempty"`
}

// Interface captures an exported interface type.
type Interface struct {
	// Methods holds the method set of the interface.
	Methods map[string]Func `json:"methods,omitempty"`
	// Embedded lists the names of embedded interfaces.
	Embedded []string `json:"embedded,omitempty"`
}

// Package captures all exported symbols in a single Go package.
type Package struct {
	// Funcs holds exported package-level functions.
	Funcs map[string]Func `json:"funcs,omitempty"`
	// Types holds exported named types and structs.
	Types map[string]Type `json:"types,omitempty"`
	// Interfaces holds exported interface types.
	Interfaces map[string]Interface `json:"interfaces,omitempty"`
	// Consts holds exported constants.
	Consts map[string]Value `json:"consts,omitempty"`
	// Vars holds exported variables.
	Vars map[string]Value `json:"vars,omitempty"`
}

// Snapshot captures the complete exported API surface of a Go module.
type Snapshot struct {
	// Module is the Go module path from go.mod.
	Module string `json:"module"`
	// Version is the semver version this snapshot represents.
	Version string `json:"version"`
	// GoVersion is the Go toolchain version from go.mod.
	GoVersion string `json:"go_version"`
	// CreatedAt is the UTC timestamp when the snapshot was created (RFC3339).
	CreatedAt string `json:"created_at"`
	// Options records the tracking flags active when the snapshot was created.
	Options Options `json:"options"`
	// Packages maps import path to the package's exported symbols.
	Packages map[string]Package `json:"packages"`
}

// Load reads and deserializes a Snapshot from the given file path.
// It returns a wrapped os.ErrNotExist if the file does not exist, allowing
// callers to check with errors.Is(err, os.ErrNotExist).
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("snapshot file %q not found: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("reading snapshot file %q: %w", path, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parsing snapshot file %q: %w", path, err)
	}
	return &snap, nil
}

// Save serializes the Snapshot as indented JSON and writes it to the given path,
// creating or overwriting the file with mode 0644.
func (s *Snapshot) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing snapshot to %q: %w", path, err)
	}
	return nil
}
