// Package differ compares two Snapshots and classifies each change as a
// MAJOR, MINOR, or PATCH semver bump according to Go API compatibility rules.
//
// The key Go-specific rule is that adding a method to an interface is a
// MAJOR (breaking) change because it breaks all existing implementors.
//
// Basic usage:
//
//	result := differ.Diff(oldSnap, newSnap)
//	fmt.Println(result.MaxBump, result.NextVersion)
package differ

import (
	"fmt"

	"github.com/harrydayexe/SemVerify/internal/snapshot"
	"github.com/harrydayexe/SemVerify/internal/version"
)

// ChangeKind classifies the nature of an API change.
type ChangeKind string

const (
	// ChangeAdded indicates a new symbol was added (MINOR bump).
	ChangeAdded ChangeKind = "added"
	// ChangeRemoved indicates an existing symbol was removed (MAJOR bump).
	ChangeRemoved ChangeKind = "removed"
	// ChangeChanged indicates a symbol's signature or type changed (usually MAJOR).
	ChangeChanged ChangeKind = "changed"
	// ChangeDeprecated indicates a symbol was marked as deprecated (MINOR bump).
	ChangeDeprecated ChangeKind = "deprecated"
)

// Change describes a single API change between two snapshots.
type Change struct {
	// Package is the import path of the affected package.
	Package string `json:"package"`
	// Symbol is the qualified name of the affected symbol.
	Symbol string `json:"symbol"`
	// Kind classifies the type of change.
	Kind ChangeKind `json:"kind"`
	// Bump is the minimum semver bump this change requires.
	Bump version.BumpKind `json:"bump"`
	// Description is a human-readable summary of the change.
	Description string `json:"description"`
}

// Result holds the complete output of a diff operation.
type Result struct {
	// Changes is the list of individual API changes.
	Changes []Change `json:"changes"`
	// MaxBump is the highest bump level required across all changes.
	MaxBump version.BumpKind `json:"max_bump"`
	// NextVersion is the computed next version string based on MaxBump.
	// Empty if the old snapshot has no parseable version.
	NextVersion string `json:"next_version,omitempty"`
}

// Diff compares two snapshots and returns a Result describing all API changes.
// It parses old.Version to compute NextVersion; if the version is unparseable,
// NextVersion is left empty.
func Diff(old, new *snapshot.Snapshot) Result {
	var changes []Change

	// Collect all package paths from both snapshots.
	allPkgs := make(map[string]struct{})
	for p := range old.Packages {
		allPkgs[p] = struct{}{}
	}
	for p := range new.Packages {
		allPkgs[p] = struct{}{}
	}

	for pkgPath := range allPkgs {
		oldPkg, inOld := old.Packages[pkgPath]
		newPkg, inNew := new.Packages[pkgPath]

		switch {
		case inOld && !inNew:
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      pkgPath,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("package %s removed", pkgPath),
			})
		case !inOld && inNew:
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      pkgPath,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("package %s added", pkgPath),
			})
		default:
			changes = append(changes, diffPackage(pkgPath, oldPkg, newPkg, old.Options)...)
		}
	}

	// Compute MaxBump.
	maxBump := version.BumpNone
	for _, c := range changes {
		if c.Bump > maxBump {
			maxBump = c.Bump
		}
	}

	// Compute NextVersion.
	var nextVersion string
	if v, err := version.Parse(old.Version); err == nil {
		nextVersion = v.Bump(maxBump).String()
	}

	return Result{
		Changes:     changes,
		MaxBump:     maxBump,
		NextVersion: nextVersion,
	}
}

// diffPackage compares the symbols in a single package and returns all changes.
// Nil maps are initialized to empty maps to prevent panics.
func diffPackage(pkgPath string, old, new snapshot.Package, opts snapshot.Options) []Change {
	// Initialize nil maps to avoid nil-map panics during iteration.
	if old.Funcs == nil {
		old.Funcs = map[string]snapshot.Func{}
	}
	if new.Funcs == nil {
		new.Funcs = map[string]snapshot.Func{}
	}
	if old.Types == nil {
		old.Types = map[string]snapshot.Type{}
	}
	if new.Types == nil {
		new.Types = map[string]snapshot.Type{}
	}
	if old.Interfaces == nil {
		old.Interfaces = map[string]snapshot.Interface{}
	}
	if new.Interfaces == nil {
		new.Interfaces = map[string]snapshot.Interface{}
	}
	if old.Consts == nil {
		old.Consts = map[string]snapshot.Value{}
	}
	if new.Consts == nil {
		new.Consts = map[string]snapshot.Value{}
	}
	if old.Vars == nil {
		old.Vars = map[string]snapshot.Value{}
	}
	if new.Vars == nil {
		new.Vars = map[string]snapshot.Value{}
	}

	var changes []Change
	changes = append(changes, diffFuncs(pkgPath, old.Funcs, new.Funcs)...)
	changes = append(changes, diffTypes(pkgPath, old.Types, new.Types, opts)...)
	changes = append(changes, diffInterfaces(pkgPath, old.Interfaces, new.Interfaces)...)
	changes = append(changes, diffValues(pkgPath, "const", old.Consts, new.Consts, opts)...)
	changes = append(changes, diffValues(pkgPath, "var", old.Vars, new.Vars, opts)...)
	return changes
}

// diffFuncs compares package-level functions between two snapshots.
func diffFuncs(pkgPath string, old, new map[string]snapshot.Func) []Change {
	var changes []Change
	for name := range old {
		if _, exists := new[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("function %s removed", name),
			})
		}
	}
	for name := range new {
		if _, exists := old[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("function %s added", name),
			})
		}
	}
	for name, oldFn := range old {
		if newFn, exists := new[name]; exists {
			changes = append(changes, diffFuncSignature(pkgPath, name, oldFn, newFn)...)
		}
	}
	return changes
}

// diffFuncSignature compares the signature of two functions with the same name.
// It is reused for package functions, struct methods, and interface methods.
func diffFuncSignature(pkgPath, name string, old, new snapshot.Func) []Change {
	var changes []Change

	if len(old.Params) != len(new.Params) {
		changes = append(changes, Change{
			Package:     pkgPath,
			Symbol:      name,
			Kind:        ChangeChanged,
			Bump:        version.BumpMajor,
			Description: fmt.Sprintf("%s: parameter count changed from %d to %d", name, len(old.Params), len(new.Params)),
		})
	} else {
		for i, op := range old.Params {
			np := new.Params[i]
			if op.Type != np.Type {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeChanged,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("%s: parameter %d type changed from %s to %s", name, i+1, op.Type, np.Type),
				})
			}
		}
	}

	if len(old.Returns) != len(new.Returns) {
		changes = append(changes, Change{
			Package:     pkgPath,
			Symbol:      name,
			Kind:        ChangeChanged,
			Bump:        version.BumpMajor,
			Description: fmt.Sprintf("%s: return count changed from %d to %d", name, len(old.Returns), len(new.Returns)),
		})
	} else {
		for i, or_ := range old.Returns {
			nr := new.Returns[i]
			if or_.Type != nr.Type {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeChanged,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("%s: return %d type changed from %s to %s", name, i+1, or_.Type, nr.Type),
				})
			}
		}
	}

	if old.Variadic != new.Variadic {
		changes = append(changes, Change{
			Package:     pkgPath,
			Symbol:      name,
			Kind:        ChangeChanged,
			Bump:        version.BumpMajor,
			Description: fmt.Sprintf("%s: variadic status changed", name),
		})
	}

	return changes
}

// diffTypes compares named types and structs between two packages.
func diffTypes(pkgPath string, old, new map[string]snapshot.Type, opts snapshot.Options) []Change {
	var changes []Change

	for name := range old {
		if _, exists := new[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("type %s removed", name),
			})
		}
	}
	for name := range new {
		if _, exists := old[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("type %s added", name),
			})
		}
	}

	for name, oldType := range old {
		newType, exists := new[name]
		if !exists {
			continue
		}

		if oldType.Kind != newType.Kind {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("type %s kind changed from %s to %s", name, oldType.Kind, newType.Kind),
			})
			continue
		}

		if oldType.Underlying != newType.Underlying && oldType.Kind != "struct" {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("type %s underlying type changed from %s to %s", name, oldType.Underlying, newType.Underlying),
			})
		}

		if oldType.Kind == "struct" {
			oldFields := oldType.Fields
			newFields := newType.Fields
			if oldFields == nil {
				oldFields = map[string]snapshot.Field{}
			}
			if newFields == nil {
				newFields = map[string]snapshot.Field{}
			}
			changes = append(changes, diffStructFields(pkgPath, name, oldFields, newFields, opts)...)
		}

		oldMethods := oldType.Methods
		newMethods := newType.Methods
		if oldMethods == nil {
			oldMethods = map[string]snapshot.Method{}
		}
		if newMethods == nil {
			newMethods = map[string]snapshot.Method{}
		}
		changes = append(changes, diffMethods(pkgPath, name, oldMethods, newMethods)...)
	}

	return changes
}

// diffStructFields compares the exported fields of a struct type.
func diffStructFields(pkgPath, typeName string, old, new map[string]snapshot.Field, opts snapshot.Options) []Change {
	var changes []Change
	symbol := typeName

	for fname := range old {
		if _, exists := new[fname]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s.%s field removed", typeName, fname),
			})
		}
	}
	for fname := range new {
		if _, exists := old[fname]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("%s.%s field added", typeName, fname),
			})
		}
	}
	for fname, oldField := range old {
		newField, exists := new[fname]
		if !exists {
			continue
		}
		if oldField.Type != newField.Type {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s.%s type changed from %s to %s", typeName, fname, oldField.Type, newField.Type),
			})
		}
		if opts.TrackFieldTags {
			changes = append(changes, diffFieldTags(pkgPath, typeName, fname, oldField.Tags, newField.Tags)...)
		}
	}

	return changes
}

// diffFieldTags compares struct tag maps for a single field.
func diffFieldTags(pkgPath, typeName, fieldName string, old, new map[string]string) []Change {
	var changes []Change
	symbol := typeName + "." + fieldName

	if old == nil {
		old = map[string]string{}
	}
	if new == nil {
		new = map[string]string{}
	}

	for key, oldVal := range old {
		newVal, exists := new[key]
		if !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s.%s tag %q removed", typeName, fieldName, key),
			})
		} else if oldVal != newVal {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s.%s tag %q changed from %q to %q", typeName, fieldName, key, oldVal, newVal),
			})
		}
	}
	for key := range new {
		if _, exists := old[key]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      symbol,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("%s.%s tag %q added", typeName, fieldName, key),
			})
		}
	}

	return changes
}

// diffMethods compares the method sets of a type.
func diffMethods(pkgPath, typeName string, old, new map[string]snapshot.Method) []Change {
	var changes []Change

	for name := range old {
		if _, exists := new[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      typeName,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s.%s method removed", typeName, name),
			})
		}
	}
	for name := range new {
		if _, exists := old[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      typeName,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("%s.%s method added", typeName, name),
			})
		}
	}
	for name, oldMethod := range old {
		newMethod, exists := new[name]
		if !exists {
			continue
		}
		sigChanges := diffFuncSignature(pkgPath, typeName+"."+name, oldMethod.Func, newMethod.Func)
		changes = append(changes, sigChanges...)
	}

	return changes
}

// diffInterfaces compares interface types between two packages.
// Note: adding a method to an interface is MAJOR because it breaks all implementors.
func diffInterfaces(pkgPath string, old, new map[string]snapshot.Interface) []Change {
	var changes []Change

	for name := range old {
		if _, exists := new[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("interface %s removed", name),
			})
		}
	}
	for name := range new {
		if _, exists := old[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("interface %s added", name),
			})
		}
	}

	for name, oldIface := range old {
		newIface, exists := new[name]
		if !exists {
			continue
		}

		oldMethods := oldIface.Methods
		newMethods := newIface.Methods
		if oldMethods == nil {
			oldMethods = map[string]snapshot.Func{}
		}
		if newMethods == nil {
			newMethods = map[string]snapshot.Func{}
		}

		for mname := range oldMethods {
			if _, exists := newMethods[mname]; !exists {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeRemoved,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("interface %s: method %s removed", name, mname),
				})
			}
		}
		// Adding a method to an interface is MAJOR — it breaks all existing implementors.
		for mname := range newMethods {
			if _, exists := oldMethods[mname]; !exists {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeAdded,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("interface %s: method %s added (breaks existing implementors)", name, mname),
				})
			}
		}
		for mname, oldFn := range oldMethods {
			newFn, exists := newMethods[mname]
			if !exists {
				continue
			}
			sigChanges := diffFuncSignature(pkgPath, name+"."+mname, oldFn, newFn)
			changes = append(changes, sigChanges...)
		}

		// Embedded interface changes — treat as MAJOR (expands or contracts the method set).
		oldEmb := toSet(oldIface.Embedded)
		newEmb := toSet(newIface.Embedded)
		for e := range oldEmb {
			if _, exists := newEmb[e]; !exists {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeRemoved,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("interface %s: embedded interface %s removed", name, e),
				})
			}
		}
		for e := range newEmb {
			if _, exists := oldEmb[e]; !exists {
				changes = append(changes, Change{
					Package:     pkgPath,
					Symbol:      name,
					Kind:        ChangeAdded,
					Bump:        version.BumpMajor,
					Description: fmt.Sprintf("interface %s: embedded interface %s added (may break existing implementors)", name, e),
				})
			}
		}
	}

	return changes
}

// diffValues compares exported constants or variables between two packages.
func diffValues(pkgPath, kind string, old, new map[string]snapshot.Value, opts snapshot.Options) []Change {
	var changes []Change

	for name := range old {
		if _, exists := new[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeRemoved,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s %s removed", kind, name),
			})
		}
	}
	for name := range new {
		if _, exists := old[name]; !exists {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeAdded,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("%s %s added", kind, name),
			})
		}
	}
	for name, oldVal := range old {
		newVal, exists := new[name]
		if !exists {
			continue
		}
		if oldVal.Type != newVal.Type {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("%s %s type changed from %s to %s", kind, name, oldVal.Type, newVal.Type),
			})
		}
		if !oldVal.Deprecated && newVal.Deprecated {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeDeprecated,
				Bump:        version.BumpMinor,
				Description: fmt.Sprintf("%s %s deprecated", kind, name),
			})
		}
		if kind == "const" && opts.TrackConstValues && oldVal.Value != newVal.Value && oldVal.Value != "" && newVal.Value != "" {
			changes = append(changes, Change{
				Package:     pkgPath,
				Symbol:      name,
				Kind:        ChangeChanged,
				Bump:        version.BumpMajor,
				Description: fmt.Sprintf("const %s value changed from %s to %s", name, oldVal.Value, newVal.Value),
			})
		}
	}

	return changes
}

// toSet converts a string slice to a set (map[string]struct{}).
func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}
