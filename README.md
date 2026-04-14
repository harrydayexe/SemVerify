# semverify — Build Plan

## What This Is

A Go CLI tool that automates semantic versioning by tracking a module's public API surface at the source level. Instead of parsing commit messages, it uses `go/ast` to extract every exported symbol, saves a snapshot manifest to disk, and diffs snapshots to determine the correct semver bump.

## Tech Stack

- Go (1.22+)
- `github.com/urfave/cli/v3` for the CLI framework
- `go/ast`, `go/parser`, `go/token` from the standard library for source parsing
- No other external dependencies

## Project Structure

```
semverify/
├── cmd/semverify/main.go          # CLI entrypoint, command wiring
├── internal/
│   ├── snapshot/snapshot.go       # Core data model + JSON serialization
│   ├── extractor/extractor.go     # go/ast parser, builds Snapshot from source
│   ├── differ/differ.go           # Compares two Snapshots, classifies changes
│   └── version/version.go         # Semver parsing, bumping, comparison
├── go.mod
└── go.sum
```

All business logic lives in `internal/`. The `cmd/` layer is a thin CLI shell that calls into the internal packages.

## Core Data Model (`internal/snapshot`)

The Snapshot is the central data structure. Everything revolves around producing, persisting, and comparing these.

The top-level `Snapshot` struct contains:
- `module` — the Go module path (from go.mod)
- `version` — the semver version this snapshot represents
- `go_version` — the Go version from go.mod
- `created_at` — UTC timestamp
- `options` — which opt-in tracking flags were active (see below)
- `packages` — a map of package import path → Package

A `Package` contains maps of:
- `funcs` — exported package-level functions
- `types` — exported struct/named types
- `interfaces` — exported interface types
- `consts` — exported constants
- `vars` — exported variables

A `Func` captures: params (name + type, in order), returns (name + type, in order), and whether variadic.

A `Method` is the same as Func but adds a receiver type string.

A `Type` captures: kind ("struct", "named", "alias"), underlying type (for named/alias), exported fields (name → type), exported methods, and embedded types.

An `Interface` captures: method set (name → Func signature), and embedded interfaces.

A `Field` captures: type string, and optionally struct tags (map of tag key → tag value).

A `Value` (for consts/vars) captures: type string, optionally the literal value, and a deprecated flag.

### Opt-In Tracking Options

Two boolean flags stored in the snapshot's `options` object:

- `track_field_tags` — when true, struct field tags (json, xml, yaml, db, etc.) are captured and diffed. A tag change is treated as a breaking change because it affects serialization contracts.
- `track_const_values` — when true, the literal values of constants are captured and diffed. A value change to an exported sentinel like `DefaultTimeout` is treated as breaking.

These are set at `init` time via CLI flags and persisted in the snapshot so that `diff` and `check` automatically inherit them.

### Serialization

The snapshot file is called `.semver-snapshot.json` by default. It's written as indented JSON to the module root. It's meant to be committed to version control — it becomes the baseline for the next diff.

Provide `Load(path) (*Snapshot, error)` and `Save(path) error` functions.

## Extractor (`internal/extractor`)

This is the most complex package. It walks a Go module's source tree using `go/ast` and `go/parser` and builds a Snapshot of all exported symbols.

### What to extract

| Symbol type      | What to capture                                                       |
|------------------|-----------------------------------------------------------------------|
| Function         | name, params (types + order), return types, variadic                  |
| Method           | receiver type, name, params, returns, variadic                        |
| Struct           | name, exported fields (name + type + optional tags), embedded types   |
| Interface        | name, method set (names + signatures), embedded interfaces            |
| Type alias/named | name, underlying type                                                 |
| Const            | name, type, optional value                                            |
| Var              | name, type                                                            |

### What to skip

- Unexported symbols (lowercase first letter)
- Test files (`_test.go`)
- `internal/` directories (they're not part of the public API)
- `vendor/` and `testdata/` directories
- `cmd/` directories (binaries, not library API)
- Hidden directories (`.` prefix)

### Implementation approach

1. Read `go.mod` to get the module path and Go version.
2. `filepath.Walk` the module root, skipping excluded directories.
3. For each directory, call `parser.ParseDir` with a filter that excludes test files. Use `parser.ParseComments` to capture doc comments for deprecation detection.
4. Iterate over `ast.File.Decls`:
   - `*ast.FuncDecl` — if exported, extract as a function or method (check `Recv` field).
   - `*ast.GenDecl` — iterate specs:
     - `*ast.TypeSpec` — branch on the type expression: `*ast.StructType`, `*ast.InterfaceType`, or other (named/alias).
     - `*ast.ValueSpec` — extract as const or var depending on `GenDecl.Tok`.
5. Write a `typeExprToString(ast.Expr) string` helper that recursively converts AST type expressions to readable strings. Handle: `*ast.Ident`, `*ast.StarExpr`, `*ast.SelectorExpr`, `*ast.ArrayType`, `*ast.MapType`, `*ast.InterfaceType`, `*ast.Ellipsis`, `*ast.FuncType`, `*ast.ChanType`, `*ast.IndexExpr` (generics).
6. For deprecation: check if a symbol's doc comment contains "Deprecated:" (Go convention).
7. For struct tags: parse the raw tag string using `reflect.StructTag` and extract known keys (json, xml, yaml, toml, db, mapstructure, validate, binding, form, query, param, header). Only populate the tags map when `track_field_tags` is enabled.

### Key detail

When extracting methods, the receiver type entry may not exist yet (methods can appear before the type declaration in the AST walk). Create the Type entry on first encounter if it doesn't exist, then merge fields/kind in later when the TypeSpec is processed.

## Differ (`internal/differ`)

Compares an old and new Snapshot and returns a list of classified changes.

### Change struct

Each change has: package path, symbol name, kind (added/removed/changed/deprecated), bump level (MAJOR/MINOR/PATCH), and a human-readable description.

### Result struct

Contains: the list of changes, the maximum bump across all changes, and optionally the computed next version string.

### Diff classification rules

**MAJOR bump:**
- Exported symbol removed (function, method, type, const, var, interface)
- Function/method parameter count changed
- Function/method parameter type changed
- Function/method return count changed
- Function/method return type changed
- Function/method variadic status changed
- Struct exported field removed
- Struct exported field type changed
- Interface method added ← **Go-specific: this breaks all existing implementors**
- Interface method removed
- Interface method signature changed
- Type kind changed (e.g. struct → interface)
- Const/var type changed
- Struct field tag changed or removed (when track_field_tags is on)
- Const value changed (when track_const_values is on)
- Entire package removed

**MINOR bump:**
- New exported function/method added
- New exported type added
- New exported const/var added
- New exported field added to struct
- Symbol marked deprecated (via doc comment)
- New struct field tag added (when track_field_tags is on)
- Entire new package added

**PATCH bump (inferred):**
- No public API surface changes detected

### Implementation approach

1. Collect all package names from both snapshots.
2. Handle whole-package additions/removals.
3. For packages that exist in both, diff each symbol category:
   - Functions: check removed, added, then signature changes on common names.
   - Types: check removed, added, then for common types: kind change, field diff, method diff.
   - Interfaces: check removed, added, then for common interfaces: method removed/added/changed. Remember: adding to an interface is MAJOR.
   - Consts/Vars: check removed, added, then type/value/deprecation changes.
4. Track the maximum bump seen across all changes. That's the overall recommended bump.

Factor out a `diffFuncSignature` helper that compares params and returns — reuse it for package functions, struct methods, and interface methods.

Initialize nil maps to empty maps at the start of each diff function to avoid nil-map panics.

## Version (`internal/version`)

Semver parsing and bump logic.

### Semver struct

Fields: Major, Minor, Patch (int), PreRelease (string), Build (string).

### Functions

- `Parse(string) (Semver, error)` — parse a version string. Accept optional "v" prefix. Split off build metadata (+), then pre-release (-), then the three dot-separated integers.
- `String() string` — format back to string (no "v" prefix).
- `IsInitialDevelopment() bool` — returns true if Major == 0.
- `Bump(BumpKind) Semver` — returns a new Semver with the bump applied. MAJOR: major+1, minor=0, patch=0. MINOR: minor+1, patch=0. PATCH: patch+1. Pre-release and build metadata are dropped on any bump.

### BumpKind enum

`BumpNone`, `BumpPatch`, `BumpMinor`, `BumpMajor` — as an int type so they can be compared with `>` to find the maximum.

## CLI (`cmd/semverify/main.go`)

Uses `github.com/urfave/cli/v3`. The root is a `*cli.Command` (v3 has no `cli.NewApp()`). Actions have signature `func(context.Context, *cli.Command) error`.

### Global flags

- `--dir` (string, default ".") — path to the Go module root
- `--snapshot-file` (string, default ".semver-snapshot.json") — path to the snapshot file

### Commands

#### `semverify init`

First-time setup. Extracts the API surface and writes the initial snapshot.

Flags:
- `--version` (string, default "0.1.0") — initial version
- `--track-tags` (bool) — enable struct field tag tracking
- `--track-values` (bool) — enable const/var value tracking

Behavior:
1. Validate the version string.
2. Run the extractor with the specified options.
3. Set version and timestamp on the snapshot.
4. Save to the snapshot file path.
5. Print summary: version, package count, symbol count, file path.

#### `semverify snapshot`

Re-captures the surface (e.g. after a release to update the baseline).

Flags:
- `--version` (string) — version to tag the snapshot with
- `--stdout` (bool) — print to stdout instead of writing to file

Behavior:
1. Load existing snapshot to inherit opt-in options.
2. Run the extractor.
3. Optionally set version from flag.
4. Save or print to stdout.

#### `semverify diff`

Compares live source against the committed snapshot.

Flags:
- `--json` (bool) — output as JSON

Behavior:
1. Load the previous snapshot (error if not found, tell user to run init).
2. Extract current surface, inheriting options from the old snapshot.
3. Run the differ.
4. Calculate next version by parsing old version and applying the max bump.
5. Output: either JSON or human-readable list of changes with icons (+, -, ~, !) and the recommended version bump.

#### `semverify check --proposed <version>`

CI gate command. Validates a proposed version against the actual API changes.

Flags:
- `--proposed` (string, required) — the version you intend to release

Behavior:
1. Load old snapshot, extract current surface, run differ.
2. Calculate the minimum required version.
3. If the old version is 0.x.x: print info but don't fail (semver rules are relaxed during initial development).
4. If 1.0.0+: compare proposed vs minimum. Exit non-zero if the proposed version is too low (e.g. breaking changes present but no major bump).
5. Print pass/fail with details.

## Tests to Write

Create a `testdata/` directory at the project root containing small fake Go modules that the tests can point the extractor at.

### Version tests (`internal/version/version_test.go`)
- Parse valid versions: "1.2.3", "v1.2.3", "0.1.0", "1.0.0-alpha", "1.0.0-alpha+build.123"
- Parse invalid versions: "1.2", "abc", "1.2.3.4"
- Bump: 1.2.3 + PATCH = 1.2.4, + MINOR = 1.3.0, + MAJOR = 2.0.0
- Bump drops pre-release: 1.0.0-alpha + PATCH = 1.0.1
- IsInitialDevelopment: 0.x.y = true, 1.x.y = false

### Extractor tests (`internal/extractor/extractor_test.go`)
- Create a temp directory with a go.mod and a .go file containing exported functions, structs, interfaces, consts, vars, and unexported symbols.
- Assert the snapshot contains all exported symbols and none of the unexported ones.
- Assert methods are attached to their correct types.
- Assert interface methods are captured.
- Assert internal/ packages are excluded.
- Assert test files are excluded.

### Differ tests (`internal/differ/differ_test.go`)
- Build two snapshots programmatically (no need for filesystem).
- Test each classification rule:
  - Remove a function → MAJOR
  - Add a function → MINOR
  - Change a param type → MAJOR
  - Add a param → MAJOR
  - Change return type → MAJOR
  - Add field to struct → MINOR
  - Remove field from struct → MAJOR
  - Add method to interface → MAJOR (the Go-specific case)
  - Remove method from interface → MAJOR
  - Mark something deprecated → MINOR
  - No changes → NONE
- Test that MaxBump is the highest bump across all changes.
- Test that field tag diff works when opt-in is on and is skipped when off.

### Integration tests
- Run the full init → make changes → diff → check pipeline against a testdata module.

## Build Order

Execute in this order because each step depends on the previous:

1. `internal/snapshot` — the data model, Load/Save. No dependencies on other internal packages.
2. `internal/version` — semver parsing and bumping. No dependencies on other internal packages.
3. `internal/version` tests — validate parse/bump logic before anything uses it.
4. `internal/extractor` — depends on snapshot. The most complex package.
5. `internal/extractor` tests — validate against testdata Go modules.
6. `internal/differ` — depends on snapshot and version.
7. `internal/differ` tests — validate all classification rules.
8. `cmd/semverify/main.go` — the CLI shell. Depends on all internal packages.
9. Integration tests.
