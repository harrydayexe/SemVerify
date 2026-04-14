// Package extractor walks a Go module's source tree using go/ast and builds
// a Snapshot of all exported symbols.
//
// It skips unexported symbols, test files, and directories that are not part
// of a module's public API surface (internal/, vendor/, testdata/, cmd/).
//
// Basic usage:
//
//	snap, err := extractor.Extract(extractor.ExtractOptions{
//	    ModuleDir: ".",
//	})
package extractor

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/harrydayexe/SemVerify/internal/snapshot"
)

// ExtractOptions configures an extraction run.
type ExtractOptions struct {
	// ModuleDir is the path to the root of the Go module.
	ModuleDir string
	// TrackFieldTags enables capturing struct field tags.
	TrackFieldTags bool
	// TrackConstValues enables capturing exported constant literal values.
	TrackConstValues bool
}

// knownTagKeys is the set of struct tag keys that semverify tracks.
var knownTagKeys = []string{
	"json", "xml", "yaml", "toml", "db", "mapstructure",
	"validate", "binding", "form", "query", "param", "header",
}

// Extract walks opts.ModuleDir and returns a Snapshot of all exported symbols
// in the module's public API surface. It reads go.mod for the module path and
// Go version, and skips internal/, vendor/, testdata/, cmd/, hidden dirs, and
// test files.
func Extract(opts ExtractOptions) (*snapshot.Snapshot, error) {
	modulePath, goVersion, err := readModuleInfo(opts.ModuleDir)
	if err != nil {
		return nil, fmt.Errorf("reading module info: %w", err)
	}

	dirs, err := collectPackageDirs(opts.ModuleDir)
	if err != nil {
		return nil, fmt.Errorf("collecting package dirs: %w", err)
	}

	fset := token.NewFileSet()
	packages := make(map[string]snapshot.Package)

	for _, dir := range dirs {
		importPath, pkg, err := extractPackage(fset, dir, modulePath, opts.ModuleDir, opts)
		if err != nil {
			return nil, fmt.Errorf("extracting package %q: %w", dir, err)
		}
		if pkg != nil {
			packages[importPath] = *pkg
		}
	}

	snap := &snapshot.Snapshot{
		Module:    modulePath,
		GoVersion: goVersion,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Options: snapshot.Options{
			TrackFieldTags:   opts.TrackFieldTags,
			TrackConstValues: opts.TrackConstValues,
		},
		Packages: packages,
	}
	return snap, nil
}

// readModuleInfo parses go.mod in dir to extract the module path and Go version.
func readModuleInfo(dir string) (modulePath, goVersion string, err error) {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("opening go.mod: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		} else if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("scanning go.mod: %w", err)
	}
	if modulePath == "" {
		return "", "", fmt.Errorf("no module directive found in go.mod")
	}
	return modulePath, goVersion, nil
}

// shouldSkipDir reports whether a directory name should be excluded from
// extraction. The root directory itself is never skipped.
func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "vendor", "testdata", "internal", "cmd":
		return true
	}
	return false
}

// collectPackageDirs returns all directories under root that contain Go source
// eligible for extraction, skipping excluded subtrees.
func collectPackageDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		// Never skip the root itself.
		if path == root {
			dirs = append(dirs, path)
			return nil
		}
		if shouldSkipDir(d.Name()) {
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs, err
}

// extractPackage parses all non-test Go files in dir and returns the package's
// import path and extracted symbols.
func extractPackage(fset *token.FileSet, dir, modulePath, rootDir string, opts ExtractOptions) (string, *snapshot.Package, error) {
	// Compute import path relative to root.
	rel, err := filepath.Rel(rootDir, dir)
	if err != nil {
		return "", nil, err
	}
	var importPath string
	if rel == "." {
		importPath = modulePath
	} else {
		importPath = modulePath + "/" + filepath.ToSlash(rel)
	}

	// Filter out test files.
	filter := func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}

	pkgs, err := parser.ParseDir(fset, dir, filter, parser.ParseComments)
	if err != nil {
		// Non-fatal: dir may contain no .go files.
		return importPath, nil, nil
	}
	if len(pkgs) == 0 {
		return importPath, nil, nil
	}

	pkg := &snapshot.Package{
		Funcs:      make(map[string]snapshot.Func),
		Types:      make(map[string]snapshot.Type),
		Interfaces: make(map[string]snapshot.Interface),
		Consts:     make(map[string]snapshot.Value),
		Vars:       make(map[string]snapshot.Value),
	}

	for _, astPkg := range pkgs {
		for _, file := range astPkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					extractFunc(pkg, d, opts)
				case *ast.GenDecl:
					extractGenDecl(pkg, d, opts)
				}
			}
		}
	}

	// If nothing was extracted, return nil to omit from snapshot.
	if len(pkg.Funcs) == 0 && len(pkg.Types) == 0 && len(pkg.Interfaces) == 0 &&
		len(pkg.Consts) == 0 && len(pkg.Vars) == 0 {
		return importPath, nil, nil
	}

	return importPath, pkg, nil
}

// extractFunc processes a FuncDecl and adds it to pkg as either a method or a
// package-level function. If the receiver type does not yet exist in pkg.Types,
// a placeholder entry is created so methods are never lost.
func extractFunc(pkg *snapshot.Package, decl *ast.FuncDecl, opts ExtractOptions) {
	if !ast.IsExported(decl.Name.Name) {
		return
	}

	params, returns, variadic := extractFuncSignature(decl.Type)

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		// Method — extract receiver type name.
		receiverType := receiverTypeName(decl.Recv.List[0].Type)
		baseName := strings.TrimPrefix(receiverType, "*")

		// Ensure the Type entry exists (may arrive before the TypeSpec).
		if _, exists := pkg.Types[baseName]; !exists {
			pkg.Types[baseName] = snapshot.Type{
				Methods: make(map[string]snapshot.Method),
			}
		}
		entry := pkg.Types[baseName]
		if entry.Methods == nil {
			entry.Methods = make(map[string]snapshot.Method)
		}
		entry.Methods[decl.Name.Name] = snapshot.Method{
			Func:     snapshot.Func{Params: params, Returns: returns, Variadic: variadic},
			Receiver: receiverType,
		}
		pkg.Types[baseName] = entry
		return
	}

	// Package-level function.
	pkg.Funcs[decl.Name.Name] = snapshot.Func{
		Params:   params,
		Returns:  returns,
		Variadic: variadic,
	}
}

// receiverTypeName extracts the type name string from a receiver field expression,
// including the pointer star if present.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return "*" + receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		// Generic receiver: T[P] — return just the base name.
		return receiverTypeName(e.X)
	case *ast.IndexListExpr:
		// Generic receiver with multiple type params.
		return receiverTypeName(e.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// extractFuncSignature returns the params, returns, and variadic flag from a
// FuncType. Unnamed params are captured with empty Name fields.
func extractFuncSignature(ft *ast.FuncType) (params, returns []snapshot.Param, variadic bool) {
	if ft.Params != nil {
		fields := ft.Params.List
		for i, field := range fields {
			isLast := i == len(fields)-1
			typeStr := typeExprToString(field.Type)

			// Detect variadic: last param with Ellipsis type.
			if isLast {
				if ell, ok := field.Type.(*ast.Ellipsis); ok {
					variadic = true
					typeStr = typeExprToString(ell.Elt)
				}
			}

			if len(field.Names) == 0 {
				// Unnamed parameter.
				params = append(params, snapshot.Param{Type: typeStr})
			} else {
				for _, name := range field.Names {
					params = append(params, snapshot.Param{Name: name.Name, Type: typeStr})
				}
			}
		}
	}

	if ft.Results != nil {
		for _, field := range ft.Results.List {
			typeStr := typeExprToString(field.Type)
			if len(field.Names) == 0 {
				returns = append(returns, snapshot.Param{Type: typeStr})
			} else {
				for _, name := range field.Names {
					returns = append(returns, snapshot.Param{Name: name.Name, Type: typeStr})
				}
			}
		}
	}

	return params, returns, variadic
}

// extractGenDecl processes a GenDecl (type, const, or var declaration).
func extractGenDecl(pkg *snapshot.Package, decl *ast.GenDecl, opts ExtractOptions) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			extractTypeSpec(pkg, s, decl.Doc, opts)
		case *ast.ValueSpec:
			extractValueSpec(pkg, s, decl.Tok, decl.Doc, opts)
		}
	}
}

// extractTypeSpec processes a TypeSpec and adds it to pkg.Types or pkg.Interfaces.
func extractTypeSpec(pkg *snapshot.Package, spec *ast.TypeSpec, genDeclDoc *ast.CommentGroup, opts ExtractOptions) {
	if !ast.IsExported(spec.Name.Name) {
		return
	}
	name := spec.Name.Name

	switch t := spec.Type.(type) {
	case *ast.StructType:
		// Retrieve any existing entry (may have methods from prior method decls).
		existing := pkg.Types[name]
		existing.Kind = "struct"

		var embedded []string
		fields := make(map[string]snapshot.Field)

		if t.Fields != nil {
			for _, field := range t.Fields.List {
				if len(field.Names) == 0 {
					// Embedded field.
					embedded = append(embedded, typeExprToString(field.Type))
					continue
				}
				for _, fname := range field.Names {
					if !ast.IsExported(fname.Name) {
						continue
					}
					f := snapshot.Field{Type: typeExprToString(field.Type)}
					if opts.TrackFieldTags && field.Tag != nil {
						f.Tags = parseStructTags(field.Tag.Value)
					}
					fields[fname.Name] = f
				}
			}
		}

		if len(fields) > 0 {
			existing.Fields = fields
		}
		if len(embedded) > 0 {
			existing.Embedded = embedded
		}
		// Preserve any methods already discovered.
		pkg.Types[name] = existing

	case *ast.InterfaceType:
		iface := pkg.Interfaces[name]
		if iface.Methods == nil {
			iface.Methods = make(map[string]snapshot.Func)
		}

		if t.Methods != nil {
			for _, method := range t.Methods.List {
				if len(method.Names) == 0 {
					// Embedded interface.
					iface.Embedded = append(iface.Embedded, typeExprToString(method.Type))
					continue
				}
				for _, mname := range method.Names {
					if !ast.IsExported(mname.Name) {
						continue
					}
					if ft, ok := method.Type.(*ast.FuncType); ok {
						params, returns, variadic := extractFuncSignature(ft)
						iface.Methods[mname.Name] = snapshot.Func{
							Params:   params,
							Returns:  returns,
							Variadic: variadic,
						}
					}
				}
			}
		}
		pkg.Interfaces[name] = iface

	default:
		// Named type or type alias.
		existing := pkg.Types[name]
		if spec.Assign.IsValid() {
			existing.Kind = "alias"
		} else {
			existing.Kind = "named"
		}
		existing.Underlying = typeExprToString(spec.Type)
		pkg.Types[name] = existing
	}
}

// extractValueSpec processes a ValueSpec (const or var declaration).
func extractValueSpec(pkg *snapshot.Package, spec *ast.ValueSpec, tok token.Token, genDeclDoc *ast.CommentGroup, opts ExtractOptions) {
	for i, nameIdent := range spec.Names {
		if !ast.IsExported(nameIdent.Name) {
			continue
		}

		var typeStr string
		if spec.Type != nil {
			typeStr = typeExprToString(spec.Type)
		}

		var valStr string
		if opts.TrackConstValues && tok == token.CONST {
			if i < len(spec.Values) {
				if lit, ok := spec.Values[i].(*ast.BasicLit); ok {
					valStr = lit.Value
				}
			}
		}

		deprecated := isDeprecated(spec.Comment) || isDeprecated(spec.Doc) || isDeprecated(genDeclDoc)

		v := snapshot.Value{
			Type:       typeStr,
			Value:      valStr,
			Deprecated: deprecated,
		}

		if tok == token.CONST {
			pkg.Consts[nameIdent.Name] = v
		} else {
			pkg.Vars[nameIdent.Name] = v
		}
	}
}

// isDeprecated reports whether a comment group contains "Deprecated:".
func isDeprecated(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	return strings.Contains(cg.Text(), "Deprecated:")
}

// parseStructTags parses a raw struct tag string (including backtick delimiters)
// and returns a map of known tag keys to their values.
func parseStructTags(raw string) map[string]string {
	// Strip backtick delimiters.
	raw = strings.Trim(raw, "`")
	tag := reflect.StructTag(raw)

	result := make(map[string]string)
	for _, key := range knownTagKeys {
		if val := tag.Get(key); val != "" {
			// Strip options (e.g. "name,omitempty" → "name,omitempty" kept as-is for diff purposes).
			result[key] = val
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// typeExprToString converts an AST type expression to a readable Go type string.
func typeExprToString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name

	case *ast.StarExpr:
		return "*" + typeExprToString(e.X)

	case *ast.SelectorExpr:
		return typeExprToString(e.X) + "." + e.Sel.Name

	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + typeExprToString(e.Elt)
		}
		return "[" + typeExprToString(e.Len) + "]" + typeExprToString(e.Elt)

	case *ast.MapType:
		return "map[" + typeExprToString(e.Key) + "]" + typeExprToString(e.Value)

	case *ast.InterfaceType:
		if e.Methods == nil || len(e.Methods.List) == 0 {
			return "interface{}"
		}
		return "interface{...}"

	case *ast.FuncType:
		return "func(" + funcParamsToString(e.Params) + ")" + funcResultsToString(e.Results)

	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + typeExprToString(e.Value)
		case ast.RECV:
			return "<-chan " + typeExprToString(e.Value)
		default:
			return "chan " + typeExprToString(e.Value)
		}

	case *ast.Ellipsis:
		return "..." + typeExprToString(e.Elt)

	case *ast.IndexExpr:
		return typeExprToString(e.X) + "[" + typeExprToString(e.Index) + "]"

	case *ast.IndexListExpr:
		parts := make([]string, len(e.Indices))
		for i, idx := range e.Indices {
			parts[i] = typeExprToString(idx)
		}
		return typeExprToString(e.X) + "[" + strings.Join(parts, ", ") + "]"

	case *ast.ParenExpr:
		return "(" + typeExprToString(e.X) + ")"

	case *ast.StructType:
		return "struct{...}"

	case *ast.BasicLit:
		return e.Value

	default:
		return fmt.Sprintf("%T", expr)
	}
}

// funcParamsToString formats a FieldList as a comma-separated parameter type string.
func funcParamsToString(fields *ast.FieldList) string {
	if fields == nil {
		return ""
	}
	var parts []string
	for _, f := range fields.List {
		typeStr := typeExprToString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typeStr)
		} else {
			for range f.Names {
				parts = append(parts, typeStr)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// funcResultsToString formats a result FieldList as a return type string.
func funcResultsToString(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fields.List {
		typeStr := typeExprToString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typeStr)
		} else {
			for range f.Names {
				parts = append(parts, typeStr)
			}
		}
	}
	if len(parts) == 1 {
		return " " + parts[0]
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
