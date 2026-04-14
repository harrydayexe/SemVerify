package snapshot_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/harrydayexe/SemVerify/internal/snapshot"
)

func fullSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Module:    "github.com/example/mod",
		Version:   "1.2.3",
		GoVersion: "1.21",
		CreatedAt: "2024-01-01T00:00:00Z",
		Options: snapshot.Options{
			TrackFieldTags:   true,
			TrackConstValues: true,
		},
		Packages: map[string]snapshot.Package{
			"github.com/example/mod": {
				Funcs: map[string]snapshot.Func{
					"DoThing": {
						Params:  []snapshot.Param{{Name: "ctx", Type: "context.Context"}, {Name: "n", Type: "int"}},
						Returns: []snapshot.Param{{Type: "string"}, {Type: "error"}},
					},
					"Variadic": {
						Params:   []snapshot.Param{{Name: "vals", Type: "string"}},
						Variadic: true,
					},
				},
				Types: map[string]snapshot.Type{
					"MyStruct": {
						Kind: "struct",
						Fields: map[string]snapshot.Field{
							"Name": {Type: "string", Tags: map[string]string{"json": "name"}},
							"Age":  {Type: "int"},
						},
						Methods: map[string]snapshot.Method{
							"String": {Func: snapshot.Func{Returns: []snapshot.Param{{Type: "string"}}}, Receiver: "MyStruct"},
						},
						Embedded: []string{"BaseType"},
					},
					"MyAlias": {
						Kind:       "alias",
						Underlying: "string",
					},
					"MyNamed": {
						Kind:       "named",
						Underlying: "int",
					},
				},
				Interfaces: map[string]snapshot.Interface{
					"Doer": {
						Methods:  map[string]snapshot.Func{"Do": {Returns: []snapshot.Param{{Type: "error"}}}},
						Embedded: []string{"io.Reader"},
					},
				},
				Consts: map[string]snapshot.Value{
					"MaxRetries": {Type: "int", Value: "3"},
					"Deprecated": {Type: "string", Deprecated: true},
				},
				Vars: map[string]snapshot.Value{
					"DefaultTimeout": {Type: "time.Duration"},
				},
			},
		},
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	original := fullSnapshot()

	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := snapshot.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(original, loaded) {
		t.Errorf("round-trip mismatch:\ngot:  %+v\nwant: %+v", loaded, original)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := snapshot.Load("/nonexistent/path/snap.json")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json {{{{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := snapshot.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSave_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	snap := &snapshot.Snapshot{Module: "test", Packages: map[string]snapshot.Package{}}
	if err := snap.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "\n  ") {
		t.Errorf("expected indented JSON, got: %s", content)
	}
}

func TestSnapshot_EmptyMaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")

	snap := &snapshot.Snapshot{
		Module:   "test",
		Packages: map[string]snapshot.Package{
			"test": {
				// nil maps
			},
		},
	}

	if err := snap.Save(path); err != nil {
		t.Fatalf("Save with nil maps: %v", err)
	}
	loaded, err := snapshot.Load(path)
	if err != nil {
		t.Fatalf("Load after nil maps: %v", err)
	}
	pkg := loaded.Packages["test"]
	// nil maps deserialize as nil — no panic expected
	_ = pkg.Funcs
	_ = pkg.Types
	_ = pkg.Interfaces
	_ = pkg.Consts
	_ = pkg.Vars
}
