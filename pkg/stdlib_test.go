// Copyright 2026 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg_test

import (
	iofs "io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/pkg"
	"github.com/go-quicktest/qt"
)

func TestImportPaths(t *testing.T) {
	paths := pkg.ImportPaths()
	qt.Assert(t, qt.IsTrue(len(paths) > 0), qt.Commentf("no packages embedded"))
	qt.Assert(t, qt.IsTrue(slices.IsSorted(paths)), qt.Commentf("import paths are not sorted: %v", paths))
	for _, ip := range []string{"strings", "encoding/json", "tool/cli"} {
		qt.Check(t, qt.IsTrue(slices.Contains(paths, ip)), qt.Commentf("import paths are missing %q: %v", ip, paths))
	}
	_, ok := pkg.Source("no/such/package")
	qt.Assert(t, qt.IsFalse(ok), qt.Commentf("Source succeeded for a package that does not exist"))
}

// TestEmbedComplete checks that every pkg.cue file on disk is
// embedded: the go:embed patterns name each directory depth
// explicitly, so a definition file at a new depth would otherwise be
// silently omitted.
//
// TestDefsCoverEveryPackage is its counterpart in the other
// direction: every package with a generated registration has a
// hand-maintained definition file.
func TestEmbedComplete(t *testing.T) {
	var onDisk []string
	err := filepath.WalkDir(".", func(filename string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "pkg.cue" {
			return err
		}
		onDisk = append(onDisk, filepath.ToSlash(filename))
		return nil
	})
	qt.Assert(t, qt.IsNil(err))
	slices.Sort(onDisk)

	var embedded []string
	for _, ip := range pkg.ImportPaths() {
		embedded = append(embedded, ip+"/pkg.cue")
	}
	slices.Sort(embedded)
	qt.Assert(t, qt.DeepEquals(embedded, onDisk))
}

// TestDefsCoverEveryPackage checks that every package directory with
// a pkg.go registration carries a pkg.cue definition file: the
// definition files are maintained by hand, so nothing regenerates a
// missing one.
func TestDefsCoverEveryPackage(t *testing.T) {
	err := filepath.WalkDir(".", func(filename string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "pkg.go" {
			return err
		}
		dir := filepath.Dir(filename)
		_, statErr := os.Stat(filepath.Join(dir, "pkg.cue"))
		qt.Check(t, qt.IsNil(statErr), qt.Commentf("%s has no pkg.cue definition file", dir))
		return nil
	})
	qt.Assert(t, qt.IsNil(err))
}

// TestDefsMatchRegisteredPackages is the consistency check for the
// hand-maintained definition files: each pkg.cue is compared against
// the builtin package it describes. They must declare the same
// members, and the declared signatures must agree with the registered
// builtin functions on which members are functions, their number of
// parameters, their contract labels, and which parameters carry defaults; a
// declaration may be tighter than the registration, never looser in these
// derivable facts. A member may declare a validator form only when the
// evaluator treats the builtin as a validator; the reverse is not
// enforced — the evaluator promotes any builtin with a bool result,
// while the validator form follows intent, which the Go source
// declares with a pkg.Validator result.
func TestDefsMatchRegisteredPackages(t *testing.T) {
	r := runtime.New()
	ctx := cuecontext.New()
	for _, ip := range pkg.ImportPaths() {
		t.Run(ip, func(t *testing.T) {
			src, ok := pkg.Source(ip)
			qt.Assert(t, qt.IsTrue(ok), qt.Commentf("no source for %q", ip))
			f, err := parser.ParseFile(ip+"/pkg.cue", src, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.CmpEquals(f.PackageName(), path.Base(ip)))
			// The definitions must be well-formed CUE, not merely
			// syntactically valid.
			qt.Check(t, qt.IsNil(ctx.BuildFile(f).Err()), qt.Commentf("definitions do not build"))

			vertex := r.LoadBuiltin(ip)
			if vertex == nil && ip == "tool" {
				// The tool package cannot be imported and registers no
				// builtins; its definition file describes the schema
				// injected into _tool.cue files, checked for
				// well-formedness above.
				return
			}
			qt.Assert(t, qt.IsNotNil(vertex), qt.Commentf("%q is not a registered builtin package", ip))

			// A name may be declared by several fields, such as the
			// per-member declarations of uuid's variants; the sig
			// attribute can only be on the first.
			fields := make(map[string]*ast.Field)
			for _, decl := range f.Decls {
				field, ok := decl.(*ast.Field)
				if !ok {
					continue
				}
				name, _, err := ast.LabelName(field.Label)
				qt.Assert(t, qt.IsNil(err), qt.Commentf("cannot name label of %v", field.Label))
				if _, ok := fields[name]; !ok {
					fields[name] = field
				}
			}
			arcs := make(map[string]*adt.Vertex)
			for _, arc := range vertex.Arcs {
				arcs[arc.Label.SelectorString(r)] = arc
			}

			declared := slices.Sorted(maps.Keys(fields))
			registered := slices.Sorted(maps.Keys(arcs))
			qt.Check(t, qt.DeepEquals(declared, registered))

			for name, field := range fields {
				arc, ok := arcs[name]
				if !ok {
					continue // already reported above
				}
				// A builtin usable bare as a validator is registered
				// as one.
				var builtin *adt.Builtin
				bare := false
				switch v := arc.BaseValue.(type) {
				case *adt.Builtin:
					builtin = v
				case *adt.BuiltinValidator:
					builtin = v.Builtin
					bare = true
				}
				isFunc := builtin != nil
				sig, validatorForm := signatureForms(field.Value)
				if !qt.Check(t, qt.Equals(sig != nil, isFunc),
					qt.Commentf("%s: declared as a signature vs being a function", name)) {
					continue
				}
				if !isFunc {
					continue
				}
				// The validator form follows the intent the Go source
				// declares, so it may only be declared for a builtin
				// the evaluator does treat as a validator, whether bare
				// or on a partial call.
				if validatorForm {
					qt.Check(t, qt.IsTrue(bare || builtin.IsValidator(len(builtin.Params)-1)),
						qt.Commentf("%s: validator form declared, but the builtin is not usable as one", name))
				}
				params := sig.Parameters()
				if !qt.Check(t, qt.HasLen(params, len(builtin.Params)),
					qt.Commentf("%s: declared parameters vs builtin parameters", name)) {
					continue
				}
				if bare || ip == "path" {
					// Bare validators have no callable runtime signature, and path
					// remains hand-registered without generated call forms.
					qt.Check(t, qt.HasLen(builtin.Types, 0),
						qt.Commentf("%s: unexpected generated runtime signature", name))
				} else if qt.Check(t, qt.HasLen(builtin.Types, 1),
					qt.Commentf("%s: generated runtime signatures", name)) {
					runtimeSig := builtin.Types[0].Fn
					qt.Assert(t, qt.IsNotNil(runtimeSig))
					runtimeParams := runtimeSig.Params
					if qt.Check(t, qt.HasLen(runtimeParams, len(params)),
						qt.Commentf("%s: authored vs generated runtime parameters", name)) {
						for i, p := range params {
							want := ""
							if p.Label != nil {
								label, isIdent, err := ast.LabelName(p.Label)
								qt.Assert(t, qt.IsNil(err))
								qt.Assert(t, qt.IsTrue(isIdent))
								if label != "_" {
									want = label
								}
							}
							got := ""
							if label := runtimeParams[i].Label; label != adt.InvalidLabel {
								got = label.SelectorString(r)
							}
							qt.Check(t, qt.Equals(got, want), qt.Commentf(
								"%s: authored parameter %d label vs generated runtime label", name, i+1))
						}
					}
				}
				for i, p := range params {
					// A declared default is written with "=" on the parameter;
					// it must agree with whether the registered builtin
					// carries a default for the same parameter.
					hasDefault := builtin.Params[i].Default() != nil
					qt.Check(t, qt.Equals(p.Default != nil, hasDefault),
						qt.Commentf("%s: parameter %d declared default vs builtin parameter having one", name, i+1))
				}
			}
		})
	}
}

// TestValidatorDetection pins one validator declaration. The generator
// detects validator intent through the pkg.Validator result alias in
// the Go signatures; were that detection to degrade, every validator
// would lose its validator form while every other check in this
// package still passed.
func TestValidatorDetection(t *testing.T) {
	src, ok := pkg.Source("strings")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.StringContains(string(src),
		"MinRunes: (func(min: int) -> validator(string)) | (func(s: string, min: int) -> bool)"))
}

// TestPreciseParamTypes pins one hand-declared parameter type: the
// definition files may declare types tighter than a Go signature can
// express — for cue.Value, an unhelpful _ — such as net's #IP. The
// pin guards against a mechanical regeneration flattening them.
func TestPreciseParamTypes(t *testing.T) {
	src, ok := pkg.Source("net")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.StringContains(string(src),
		"IPv4: validator(#IP) | (func(ip: #IP) -> bool)"))
	// The referenced type is itself a member, declared in the package's
	// supplemental CUE, so the definition file is self-contained.
	qt.Assert(t, qt.StringContains(string(src), "#IP: string | bytes | [...int]"))
}

// signatureForms extracts a member's declared signature: the ordinary
// call form, and whether a validator form is declared alongside it —
// the generated files declare a validator as a disjunction with the
// validator form first and the call form last.
func signatureForms(v ast.Expr) (callForm *ast.Func, validatorForm bool) {
	v = unparen(v)
	bin, ok := v.(*ast.BinaryExpr)
	if !ok || bin.Op != token.OR {
		f, _ := v.(*ast.Func)
		return f, false
	}
	callForm, _ = unparen(bin.Y).(*ast.Func)
	return callForm, isValidatorForm(unparen(bin.X))
}

// isValidatorForm reports whether an expression is a validator form:
// the type validator(T), or a function returning one.
func isValidatorForm(v ast.Expr) bool {
	if f, ok := v.(*ast.Func); ok {
		return f.Ret != nil && isValidatorForm(unparen(f.Ret))
	}
	call, ok := v.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "validator"
}

// unparen removes any parentheses wrapping an expression.
func unparen(v ast.Expr) ast.Expr {
	for {
		p, ok := v.(*ast.ParenExpr)
		if !ok {
			return v
		}
		v = p.X
	}
}
