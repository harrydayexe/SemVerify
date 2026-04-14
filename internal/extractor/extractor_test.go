package extractor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/harrydayexe/SemVerify/internal/extractor"
)

// writeModule creates a minimal Go module in dir with the given source files.
// files maps relative file path to content.
func writeModule(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/testmod\n\ngo 1.21\n"), 0644); err != nil {
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


// We'll access the snapshot directly since the struct is exported.

func TestExtract_BasicModule(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"main.go": `package main

// Exported is exported.
func Exported() {}

// unexported is not exported.
func unexported() {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg, ok := snap.Packages["example.com/testmod"]
	if !ok {
		t.Fatal("root package not found in snapshot")
	}
	if _, ok := pkg.Funcs["Exported"]; !ok {
		t.Error("Exported function should be in snapshot")
	}
	if _, ok := pkg.Funcs["unexported"]; ok {
		t.Error("unexported function should not be in snapshot")
	}
}

func TestExtract_Methods(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

type MyType struct{}

// ValueMethod is a value receiver method.
func (m MyType) ValueMethod() string { return "" }

// PtrMethod is a pointer receiver method.
func (m *MyType) PtrMethod() error { return nil }

// unexportedMethod should not appear.
func (m *MyType) unexportedMethod() {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ, ok := pkg.Types["MyType"]
	if !ok {
		t.Fatal("MyType not found")
	}
	if _, ok := typ.Methods["ValueMethod"]; !ok {
		t.Error("ValueMethod not found on MyType")
	}
	if _, ok := typ.Methods["PtrMethod"]; !ok {
		t.Error("PtrMethod not found on MyType")
	}
	if _, ok := typ.Methods["unexportedMethod"]; ok {
		t.Error("unexportedMethod should not appear")
	}

	vm := typ.Methods["ValueMethod"]
	if vm.Receiver != "MyType" {
		t.Errorf("ValueMethod receiver = %q, want %q", vm.Receiver, "MyType")
	}
	pm := typ.Methods["PtrMethod"]
	if pm.Receiver != "*MyType" {
		t.Errorf("PtrMethod receiver = %q, want %q", pm.Receiver, "*MyType")
	}
}

func TestExtract_MethodBeforeType(t *testing.T) {
	dir := t.TempDir()
	// Method decl appears before the type decl in the same file.
	writeModule(t, dir, map[string]string{
		"types.go": `package main

func (e *EarlyMethod) Method() string { return "" }

type EarlyMethod struct {
	Name string
}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ, ok := pkg.Types["EarlyMethod"]
	if !ok {
		t.Fatal("EarlyMethod not found")
	}
	if _, ok := typ.Methods["Method"]; !ok {
		t.Error("Method not found on EarlyMethod")
	}
	if _, ok := typ.Fields["Name"]; !ok {
		t.Error("Name field not found on EarlyMethod")
	}
}

func TestExtract_Struct(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

type Config struct {
	Host     string
	Port     int
	password string
}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ, ok := pkg.Types["Config"]
	if !ok {
		t.Fatal("Config not found")
	}
	if _, ok := typ.Fields["Host"]; !ok {
		t.Error("Host field not found")
	}
	if _, ok := typ.Fields["Port"]; !ok {
		t.Error("Port field not found")
	}
	if _, ok := typ.Fields["password"]; ok {
		t.Error("unexported password field should not appear")
	}
}

func TestExtract_StructTags(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

type User struct {
	Name  string ` + "`json:\"name\" xml:\"Name\"`" + `
	Email string ` + "`json:\"email,omitempty\"`" + `
}
`,
	})

	t.Run("tags enabled", func(t *testing.T) {
		opts := extractor.ExtractOptions{ModuleDir: dir, TrackFieldTags: true}
		snap, err := extractor.Extract(opts)
		if err != nil {
			t.Fatal(err)
		}
		pkg := snap.Packages["example.com/testmod"]
		nameField := pkg.Types["User"].Fields["Name"]
		if nameField.Tags["json"] != "name" {
			t.Errorf("json tag for Name = %q, want %q", nameField.Tags["json"], "name")
		}
		if nameField.Tags["xml"] != "Name" {
			t.Errorf("xml tag for Name = %q, want %q", nameField.Tags["xml"], "Name")
		}
		emailField := pkg.Types["User"].Fields["Email"]
		if emailField.Tags["json"] != "email,omitempty" {
			t.Errorf("json tag for Email = %q, want %q", emailField.Tags["json"], "email,omitempty")
		}
	})

	t.Run("tags disabled", func(t *testing.T) {
		opts := extractor.ExtractOptions{ModuleDir: dir, TrackFieldTags: false}
		snap, err := extractor.Extract(opts)
		if err != nil {
			t.Fatal(err)
		}
		pkg := snap.Packages["example.com/testmod"]
		nameField := pkg.Types["User"].Fields["Name"]
		if len(nameField.Tags) > 0 {
			t.Error("tags should be empty when TrackFieldTags is false")
		}
	})
}

func TestExtract_Interface(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"iface.go": `package main

import "context"

type Storage interface {
	Get(ctx context.Context, key string) (string, error)
	Set(key string, value string) error
	Delete(key string)
}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	iface, ok := pkg.Interfaces["Storage"]
	if !ok {
		t.Fatal("Storage interface not found")
	}

	getMethod, ok := iface.Methods["Get"]
	if !ok {
		t.Fatal("Get method not found on Storage")
	}
	if len(getMethod.Params) != 2 {
		t.Errorf("Get params count = %d, want 2", len(getMethod.Params))
	}
	if len(getMethod.Returns) != 2 {
		t.Errorf("Get return count = %d, want 2", len(getMethod.Returns))
	}

	if _, ok := iface.Methods["Set"]; !ok {
		t.Error("Set method not found")
	}
	if _, ok := iface.Methods["Delete"]; !ok {
		t.Error("Delete method not found")
	}
}

func TestExtract_InterfaceEmbedded(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"iface.go": `package main

import "io"

type ReadWriter interface {
	io.Reader
	io.Writer
	Extra() string
}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	iface := pkg.Interfaces["ReadWriter"]
	if len(iface.Embedded) != 2 {
		t.Errorf("embedded count = %d, want 2", len(iface.Embedded))
	}
}

func TestExtract_ConstsAndVars(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"vals.go": `package main

const (
	MaxRetries int    = 3
	AppName    string = "myapp"
	unexported        = "hidden"
)

var (
	DefaultTimeout int
	counter        int
)
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	if _, ok := pkg.Consts["MaxRetries"]; !ok {
		t.Error("MaxRetries const not found")
	}
	if _, ok := pkg.Consts["AppName"]; !ok {
		t.Error("AppName const not found")
	}
	if _, ok := pkg.Consts["unexported"]; ok {
		t.Error("unexported const should not appear")
	}
	if _, ok := pkg.Vars["DefaultTimeout"]; !ok {
		t.Error("DefaultTimeout var not found")
	}
	if _, ok := pkg.Vars["counter"]; ok {
		t.Error("unexported counter var should not appear")
	}
}

func TestExtract_ConstValues(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"vals.go": `package main

const (
	MaxRetries int = 3
	AppName        = "myapp"
)
`,
	})

	t.Run("values enabled", func(t *testing.T) {
		opts := extractor.ExtractOptions{ModuleDir: dir, TrackConstValues: true}
		snap, err := extractor.Extract(opts)
		if err != nil {
			t.Fatal(err)
		}
		pkg := snap.Packages["example.com/testmod"]
		if pkg.Consts["MaxRetries"].Value != "3" {
			t.Errorf("MaxRetries value = %q, want %q", pkg.Consts["MaxRetries"].Value, "3")
		}
		if pkg.Consts["AppName"].Value != `"myapp"` {
			t.Errorf("AppName value = %q, want %q", pkg.Consts["AppName"].Value, `"myapp"`)
		}
	})

	t.Run("values disabled", func(t *testing.T) {
		opts := extractor.ExtractOptions{ModuleDir: dir, TrackConstValues: false}
		snap, err := extractor.Extract(opts)
		if err != nil {
			t.Fatal(err)
		}
		pkg := snap.Packages["example.com/testmod"]
		if pkg.Consts["MaxRetries"].Value != "" {
			t.Error("const value should be empty when TrackConstValues is false")
		}
	})
}

func TestExtract_Deprecation(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"vals.go": `package main

// OldTimeout is the old timeout.
// Deprecated: Use NewTimeout instead.
const OldTimeout = 30

// NewTimeout is the new timeout.
const NewTimeout = 60
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	if !pkg.Consts["OldTimeout"].Deprecated {
		t.Error("OldTimeout should be marked deprecated")
	}
	if pkg.Consts["NewTimeout"].Deprecated {
		t.Error("NewTimeout should not be deprecated")
	}
}

func TestExtract_TypeAlias(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

type MyString = string
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ, ok := pkg.Types["MyString"]
	if !ok {
		t.Fatal("MyString not found")
	}
	if typ.Kind != "alias" {
		t.Errorf("kind = %q, want %q", typ.Kind, "alias")
	}
	if typ.Underlying != "string" {
		t.Errorf("underlying = %q, want %q", typ.Underlying, "string")
	}
}

func TestExtract_NamedType(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

type StatusCode int
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ, ok := pkg.Types["StatusCode"]
	if !ok {
		t.Fatal("StatusCode not found")
	}
	if typ.Kind != "named" {
		t.Errorf("kind = %q, want %q", typ.Kind, "named")
	}
	if typ.Underlying != "int" {
		t.Errorf("underlying = %q, want %q", typ.Underlying, "int")
	}
}

func TestExtract_SkipInternalDir(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"api.go": `package main

func PublicFunc() {}
`,
		"internal/helper/helper.go": `package helper

func InternalFunc() {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	for pkgPath := range snap.Packages {
		if filepath.Base(pkgPath) == "helper" {
			t.Errorf("internal package %q should be excluded", pkgPath)
		}
	}
}

func TestExtract_SkipTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"api.go": `package main

func PublicFunc() {}
`,
		"api_test.go": `package main

func TestOnlyFunc() {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	if _, ok := pkg.Funcs["TestOnlyFunc"]; ok {
		t.Error("TestOnlyFunc from test file should not appear in snapshot")
	}
}

func TestExtract_Subpackages(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"api.go": `package main

func RootFunc() {}
`,
		"sub/sub.go": `package sub

func SubFunc() {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := snap.Packages["example.com/testmod"]; !ok {
		t.Error("root package not found")
	}
	if _, ok := snap.Packages["example.com/testmod/sub"]; !ok {
		t.Error("sub package not found")
	}
}

func TestExtract_Variadic(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"api.go": `package main

func Printf(format string, args ...any) {}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	fn, ok := pkg.Funcs["Printf"]
	if !ok {
		t.Fatal("Printf not found")
	}
	if !fn.Variadic {
		t.Error("Printf should be variadic")
	}
	// Last param type should be the element type, not "...any".
	last := fn.Params[len(fn.Params)-1]
	if last.Type != "any" {
		t.Errorf("variadic param type = %q, want %q", last.Type, "any")
	}
}

func TestExtract_EmbeddedStruct(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"types.go": `package main

import "sync"

type SafeMap struct {
	sync.Mutex
	data map[string]string
}
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]
	typ := pkg.Types["SafeMap"]
	found := false
	for _, e := range typ.Embedded {
		if e == "sync.Mutex" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sync.Mutex not in Embedded: %v", typ.Embedded)
	}
}

func TestExtract_FuncSignatures(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, map[string]string{
		"api.go": `package main

import "context"

func MultiParam(ctx context.Context, key string, val int) (string, error) { return "", nil }
func Unnamed(string, int) bool { return false }
`,
	})

	opts := extractor.ExtractOptions{ModuleDir: dir}
	snap, err := extractor.Extract(opts)
	if err != nil {
		t.Fatal(err)
	}

	pkg := snap.Packages["example.com/testmod"]

	mp := pkg.Funcs["MultiParam"]
	if len(mp.Params) != 3 {
		t.Errorf("MultiParam params count = %d, want 3", len(mp.Params))
	}
	if mp.Params[0].Type != "context.Context" {
		t.Errorf("first param type = %q, want %q", mp.Params[0].Type, "context.Context")
	}
	if len(mp.Returns) != 2 {
		t.Errorf("MultiParam returns count = %d, want 2", len(mp.Returns))
	}

	un := pkg.Funcs["Unnamed"]
	if len(un.Params) != 2 {
		t.Errorf("Unnamed params count = %d, want 2", len(un.Params))
	}
	if un.Params[0].Name != "" {
		t.Error("unnamed param should have empty name")
	}
}
