package differ_test

import (
	"testing"

	"github.com/harrydayexe/SemVerify/internal/differ"
	"github.com/harrydayexe/SemVerify/internal/snapshot"
	"github.com/harrydayexe/SemVerify/internal/version"
)

// makeSnap builds a minimal Snapshot with the given version and a single package.
func makeSnap(ver string, pkg snapshot.Package) *snapshot.Snapshot {
	return makeSnapOpts(ver, pkg, snapshot.Options{})
}

func makeSnapOpts(ver string, pkg snapshot.Package, opts snapshot.Options) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Module:   "example.com/mod",
		Version:  ver,
		Options:  opts,
		Packages: map[string]snapshot.Package{"example.com/mod": pkg},
	}
}

func simpleFunc(paramTypes ...string) snapshot.Func {
	params := make([]snapshot.Param, len(paramTypes))
	for i, t := range paramTypes {
		params[i] = snapshot.Param{Type: t}
	}
	return snapshot.Func{Params: params}
}

func funcWithReturn(returnTypes ...string) snapshot.Func {
	returns := make([]snapshot.Param, len(returnTypes))
	for i, t := range returnTypes {
		returns[i] = snapshot.Param{Type: t}
	}
	return snapshot.Func{Returns: returns}
}

func assertBump(t *testing.T, result differ.Result, want version.BumpKind) {
	t.Helper()
	if result.MaxBump != want {
		t.Errorf("MaxBump = %v, want %v\nchanges: %v", result.MaxBump, want, result.Changes)
	}
}

func assertChangeCount(t *testing.T, result differ.Result, want int) {
	t.Helper()
	if len(result.Changes) != want {
		t.Errorf("change count = %d, want %d\nchanges: %v", len(result.Changes), want, result.Changes)
	}
}

func assertHasChange(t *testing.T, result differ.Result, symbol string, kind differ.ChangeKind, bump version.BumpKind) {
	t.Helper()
	for _, c := range result.Changes {
		if c.Symbol == symbol && c.Kind == kind && c.Bump == bump {
			return
		}
	}
	t.Errorf("expected change {symbol=%q kind=%v bump=%v} not found in %v", symbol, kind, bump, result.Changes)
}

// --- No changes ---

func TestDiff_NoChanges(t *testing.T) {
	pkg := snapshot.Package{
		Funcs: map[string]snapshot.Func{
			"Foo": simpleFunc("int"),
		},
	}
	result := differ.Diff(makeSnap("1.0.0", pkg), makeSnap("1.0.0", pkg))
	assertBump(t, result, version.BumpNone)
	assertChangeCount(t, result, 0)
}

// --- Function changes ---

func TestDiff_FuncRemoved(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("int")}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	assertHasChange(t, result, "Foo", differ.ChangeRemoved, version.BumpMajor)
}

func TestDiff_FuncAdded(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Bar": simpleFunc()}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
	assertHasChange(t, result, "Bar", differ.ChangeAdded, version.BumpMinor)
}

func TestDiff_FuncParamCountChanged(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("int")}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("int", "string")}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_FuncParamTypeChanged(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("int")}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("string")}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_FuncReturnCountChanged(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": funcWithReturn("error")}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": funcWithReturn("string", "error")}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_FuncReturnTypeChanged(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": funcWithReturn("int")}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": funcWithReturn("string")}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_FuncVariadicChanged(t *testing.T) {
	old := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": {Params: []snapshot.Param{{Type: "string"}}, Variadic: false}}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": {Params: []snapshot.Param{{Type: "string"}}, Variadic: true}}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

// --- Struct field changes ---

func TestDiff_StructFieldAdded(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Name": {Type: "string"}}},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Name": {Type: "string"}, "Age": {Type: "int"}}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
	assertHasChange(t, result, "MyStruct", differ.ChangeAdded, version.BumpMinor)
}

func TestDiff_StructFieldRemoved(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Name": {Type: "string"}, "Age": {Type: "int"}}},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Name": {Type: "string"}}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	assertHasChange(t, result, "MyStruct", differ.ChangeRemoved, version.BumpMajor)
}

func TestDiff_StructFieldTypeChanged(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Count": {Type: "int"}}},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"MyStruct": {Kind: "struct", Fields: map[string]snapshot.Field{"Count": {Type: "int64"}}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

// --- Method changes ---

func TestDiff_MethodAdded(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"MyType": {Kind: "struct"},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"MyType": {Kind: "struct", Methods: map[string]snapshot.Method{
			"DoThing": {Func: snapshot.Func{}, Receiver: "MyType"},
		}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
	assertHasChange(t, result, "MyType", differ.ChangeAdded, version.BumpMinor)
}

func TestDiff_MethodRemoved(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"MyType": {Kind: "struct", Methods: map[string]snapshot.Method{
			"DoThing": {Func: snapshot.Func{}, Receiver: "MyType"},
		}},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"MyType": {Kind: "struct"},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	assertHasChange(t, result, "MyType", differ.ChangeRemoved, version.BumpMajor)
}

// --- Interface changes ---

func TestDiff_InterfaceMethodAdded_IsMajor(t *testing.T) {
	// This is the key Go-specific rule: adding to an interface breaks implementors.
	old := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": funcWithReturn("error")}},
	}}
	new := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{
			"Do":    funcWithReturn("error"),
			"Close": funcWithReturn("error"),
		}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	assertHasChange(t, result, "Doer", differ.ChangeAdded, version.BumpMajor)
}

func TestDiff_InterfaceMethodRemoved(t *testing.T) {
	old := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": funcWithReturn("error"), "Extra": {}}},
	}}
	new := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": funcWithReturn("error")}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	assertHasChange(t, result, "Doer", differ.ChangeRemoved, version.BumpMajor)
}

func TestDiff_InterfaceMethodSignatureChanged(t *testing.T) {
	old := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": simpleFunc("string")}},
	}}
	new := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": simpleFunc("int")}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_InterfaceRemoved(t *testing.T) {
	old := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": {}}},
	}}
	new := snapshot.Package{}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_InterfaceAdded(t *testing.T) {
	old := snapshot.Package{}
	new := snapshot.Package{Interfaces: map[string]snapshot.Interface{
		"Doer": {Methods: map[string]snapshot.Func{"Do": {}}},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
}

// --- Type kind change ---

func TestDiff_TypeKindChanged(t *testing.T) {
	old := snapshot.Package{Types: map[string]snapshot.Type{
		"Foo": {Kind: "struct"},
	}}
	new := snapshot.Package{Types: map[string]snapshot.Type{
		"Foo": {Kind: "named", Underlying: "int"},
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

// --- Const/var changes ---

func TestDiff_ConstRemoved(t *testing.T) {
	old := snapshot.Package{Consts: map[string]snapshot.Value{"MaxN": {Type: "int"}}}
	new := snapshot.Package{}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_ConstAdded(t *testing.T) {
	old := snapshot.Package{}
	new := snapshot.Package{Consts: map[string]snapshot.Value{"NewConst": {Type: "int"}}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
}

func TestDiff_ConstTypeChanged(t *testing.T) {
	old := snapshot.Package{Consts: map[string]snapshot.Value{"MaxN": {Type: "int"}}}
	new := snapshot.Package{Consts: map[string]snapshot.Value{"MaxN": {Type: "int64"}}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_ConstDeprecated(t *testing.T) {
	old := snapshot.Package{Consts: map[string]snapshot.Value{"OldVal": {Type: "int", Deprecated: false}}}
	new := snapshot.Package{Consts: map[string]snapshot.Value{"OldVal": {Type: "int", Deprecated: true}}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMinor)
	assertHasChange(t, result, "OldVal", differ.ChangeDeprecated, version.BumpMinor)
}

// --- Const value tracking ---

func TestDiff_ConstValueChanged_TrackingOn(t *testing.T) {
	opts := snapshot.Options{TrackConstValues: true}
	old := makeSnapOpts("1.0.0", snapshot.Package{
		Consts: map[string]snapshot.Value{"Limit": {Type: "int", Value: "10"}},
	}, opts)
	new := makeSnapOpts("1.0.0", snapshot.Package{
		Consts: map[string]snapshot.Value{"Limit": {Type: "int", Value: "20"}},
	}, opts)
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_ConstValueChanged_TrackingOff(t *testing.T) {
	old := makeSnap("1.0.0", snapshot.Package{
		Consts: map[string]snapshot.Value{"Limit": {Type: "int", Value: "10"}},
	})
	new := makeSnap("1.0.0", snapshot.Package{
		Consts: map[string]snapshot.Value{"Limit": {Type: "int", Value: "20"}},
	})
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpNone)
}

// --- Field tag tracking ---

func TestDiff_FieldTagChanged_TrackingOn(t *testing.T) {
	opts := snapshot.Options{TrackFieldTags: true}
	old := makeSnapOpts("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string", Tags: map[string]string{"json": "name"}},
			}},
		},
	}, opts)
	new := makeSnapOpts("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string", Tags: map[string]string{"json": "full_name"}},
			}},
		},
	}, opts)
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_FieldTagAdded_TrackingOn(t *testing.T) {
	opts := snapshot.Options{TrackFieldTags: true}
	old := makeSnapOpts("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string"},
			}},
		},
	}, opts)
	new := makeSnapOpts("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string", Tags: map[string]string{"json": "name"}},
			}},
		},
	}, opts)
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpMinor)
}

func TestDiff_FieldTagChanged_TrackingOff(t *testing.T) {
	old := makeSnap("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string", Tags: map[string]string{"json": "name"}},
			}},
		},
	})
	new := makeSnap("1.0.0", snapshot.Package{
		Types: map[string]snapshot.Type{
			"User": {Kind: "struct", Fields: map[string]snapshot.Field{
				"Name": {Type: "string", Tags: map[string]string{"json": "full_name"}},
			}},
		},
	})
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpNone)
}

// --- Package-level changes ---

func TestDiff_PackageRemoved(t *testing.T) {
	old := &snapshot.Snapshot{
		Version: "1.0.0",
		Packages: map[string]snapshot.Package{
			"example.com/mod":     {},
			"example.com/mod/sub": {},
		},
	}
	new := &snapshot.Snapshot{
		Version:  "1.0.0",
		Packages: map[string]snapshot.Package{"example.com/mod": {}},
	}
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpMajor)
}

func TestDiff_PackageAdded(t *testing.T) {
	old := &snapshot.Snapshot{
		Version:  "1.0.0",
		Packages: map[string]snapshot.Package{"example.com/mod": {}},
	}
	new := &snapshot.Snapshot{
		Version: "1.0.0",
		Packages: map[string]snapshot.Package{
			"example.com/mod":     {},
			"example.com/mod/sub": {},
		},
	}
	result := differ.Diff(old, new)
	assertBump(t, result, version.BumpMinor)
}

// --- MaxBump selects the highest ---

func TestDiff_MaxBumpIsHighest(t *testing.T) {
	// Mix: one MAJOR (func removed) and one MINOR (func added) — result should be MAJOR.
	old := snapshot.Package{Funcs: map[string]snapshot.Func{
		"OldFunc": simpleFunc("int"),
	}}
	new := snapshot.Package{Funcs: map[string]snapshot.Func{
		"NewFunc": simpleFunc("string"),
	}}
	result := differ.Diff(makeSnap("1.0.0", old), makeSnap("1.0.0", new))
	assertBump(t, result, version.BumpMajor)
	if len(result.Changes) < 2 {
		t.Errorf("expected at least 2 changes, got %d", len(result.Changes))
	}
}

// --- NextVersion computation ---

func TestDiff_NextVersion(t *testing.T) {
	tests := []struct {
		name        string
		oldVersion  string
		old, new    snapshot.Package
		wantNext    string
	}{
		{
			name:       "major bump",
			oldVersion: "1.2.3",
			old:        snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc("int")}},
			new:        snapshot.Package{},
			wantNext:   "2.0.0",
		},
		{
			name:       "minor bump",
			oldVersion: "1.2.3",
			old:        snapshot.Package{},
			new:        snapshot.Package{Funcs: map[string]snapshot.Func{"Bar": simpleFunc()}},
			wantNext:   "1.3.0",
		},
		{
			name:       "no changes",
			oldVersion: "1.2.3",
			old:        snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc()}},
			new:        snapshot.Package{Funcs: map[string]snapshot.Func{"Foo": simpleFunc()}},
			wantNext:   "1.2.3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := differ.Diff(makeSnap(tc.oldVersion, tc.old), makeSnap(tc.oldVersion, tc.new))
			if result.NextVersion != tc.wantNext {
				t.Errorf("NextVersion = %q, want %q", result.NextVersion, tc.wantNext)
			}
		})
	}
}

// --- Nil map safety ---

func TestDiff_NilMapsNoPanic(t *testing.T) {
	// Packages with all nil maps should not panic.
	old := &snapshot.Snapshot{
		Version:  "1.0.0",
		Packages: map[string]snapshot.Package{"example.com/mod": {}},
	}
	new := &snapshot.Snapshot{
		Version:  "1.0.0",
		Packages: map[string]snapshot.Package{"example.com/mod": {}},
	}
	// Should not panic.
	result := differ.Diff(old, new)
	if result.MaxBump != version.BumpNone {
		t.Errorf("expected BumpNone for empty packages, got %v", result.MaxBump)
	}
}
