// Copyright 2024 CUE Authors
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

package gotypes

import (
	"bytes"
	"fmt"
	goformat "go/format"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/mod/module"
)

// Generate produces Go type definitions from exported CUE definitions.
// See the help text for `cue help exp gengotypes`.
func Generate(ctx *cue.Context, insts ...*build.Instance) error {
	// record which package instances have already been generated
	instDone := make(map[*build.Instance]bool)

	goPkgNamesDoneByDir := make(map[string]string)

	// ensure we don't modify the parameter slice
	insts = slices.Clip(insts)
	for len(insts) > 0 { // we append imports to this list
		inst := insts[0]
		insts = insts[1:]
		if err := inst.Err; err != nil {
			return err
		}
		if instDone[inst] {
			continue
		}
		instDone[inst] = true

		instVal := ctx.BuildInstance(inst)
		if err := instVal.Validate(); err != nil {
			return err
		}
		g := generator{
			pkgRoot:    instVal,
			defsAdded:  make(map[string]bool),
			importedAs: make(map[string]string),
		}
		iter, err := instVal.Fields(cue.Definitions(true))
		if err != nil {
			return err
		}
		for iter.Next() {
			if sel := iter.Selector(); sel.IsDefinition() {
				g.addDef(cue.MakePath(sel))
			}
		}
		for len(g.defsList) > 0 {
			path := g.defsList[0]
			g.defsList = g.defsList[1:]

			val := instVal.LookupPath(path)
			goAttr := val.Attribute("go")
			goName := goNameFromPath(path, true)
			if goName == "" {
				return fmt.Errorf("unexpected path in defsList: %q", path.String())
			}
			if s, _ := goAttr.String(0); s != "" {
				goName = s
			}

			g.emitDocs(goName, val.Doc())
			g.appendf("type %s ", goName)
			if err := g.emitType(val, false); err != nil {
				return err
			}
			g.appendf("\n\n")
		}

		typesBuf := g.dst
		g.dst = nil

		// TODO: we should refuse to generate for packages which are not
		// part of the main module, as they may be inside the read-only module cache.
		for _, imp := range inst.Imports {
			if !instDone[imp] && g.importedAs[imp.ImportPath] != "" {
				insts = append(insts, imp)
			}
		}

		g.appendf("// Code generated by \"cue exp gengotypes\"; DO NOT EDIT.\n\n")
		goPkgName := goPkgNameForInstance(inst, instVal)
		if prev, ok := goPkgNamesDoneByDir[inst.Dir]; ok && prev != goPkgName {
			return fmt.Errorf("cannot generate two Go packages in one directory; %s and %s", prev, goPkgName)
		} else {
			goPkgNamesDoneByDir[inst.Dir] = goPkgName
		}
		g.appendf("package %s\n\n", goPkgName)
		// TODO: imported := slices.Sorted(maps.Values(g.importedAs))
		var imported []string
		for _, path := range g.importedAs {
			imported = append(imported, path)
		}
		sort.Strings(imported)
		imported = slices.Compact(imported)
		if len(imported) > 0 {
			g.appendf("import (\n")
			for _, path := range imported {
				g.appendf("\t%q\n", path)
			}
			g.appendf(")\n")
		}
		g.appendf("%s", typesBuf)
		// The generated file is named after the CUE package, not the generated Go package,
		// as we can have multiple CUE packages in one directory all generating to one Go package.
		outpath := filepath.Join(inst.Dir, fmt.Sprintf("cue_types_%s.go", inst.PkgName))
		formatted, err := goformat.Source(g.dst)
		if err != nil {
			// Showing the generated Go code helps debug where the syntax error is.
			// This should only occur if our code generator is buggy.
			lines := bytes.Split(g.dst, []byte("\n"))
			var withLineNums []byte
			for i, line := range lines {
				withLineNums = fmt.Appendf(withLineNums, "% 4d: %s\n", i+1, line)
			}
			fmt.Fprintf(os.Stderr, "-- %s --\n%s\n--\n", filepath.ToSlash(outpath), withLineNums)
			return err
		}
		if err := os.WriteFile(outpath, formatted, 0o666); err != nil {
			return err
		}
	}
	return nil
}

// generator holds the state for generating Go code for one CUE package instance.
type generator struct {
	dst []byte

	// defsAdded records which definitions have already been added to [generator.defsList]
	// keyed by [cue.Path.String].
	defsAdded map[string]bool

	// defsList records the remaining definitions which need to be generated.
	// Any definition at the top level or underneath other definitions must be added here.
	defsList []cue.Path

	// importedAs records which CUE packages need to be imported as which Go packages in the generated Go package.
	// This is collected as we emit types, given that some CUE fields and types are omitted
	// and we don't want to end up with unused Go imports.
	//
	// The keys are full CUE import paths; the values are their resulting Go import paths.
	importedAs map[string]string

	// pkgRoot is the root value of the CUE package, necessary to tell if a referenced value
	// belongs to the current package or not.
	pkgRoot cue.Value
}

func (g *generator) appendf(format string, args ...any) {
	g.dst = fmt.Appendf(g.dst, format, args...)
}

// addDef records that the definition at the given path needs to be generated
// as it is referenced by a Go type we have already generated.
func (g *generator) addDef(path cue.Path) {
	s := path.String()
	if g.defsAdded[s] {
		return
	}
	g.defsAdded[s] = true
	g.defsList = append(g.defsList, path)
}

// emitType generates a CUE value as a Go type.
// When possible, the Go type is emitted in the form of a reference.
// Otherwise, an inline Go type expression is used.
func (g *generator) emitType(val cue.Value, optional bool) error {
	goAttr := val.Attribute("go")
	// We prefer the form @go(Name,type=pkg.Baz) as it is explicit and extensible,
	// but we are also backwards compatible with @go(Name,pkg.Baz) as emitted by `cue get go`.
	attrType, _, _ := goAttr.Lookup(1, "type")
	if attrType == "" {
		attrType, _ = goAttr.String(1)
	}
	if attrType != "" {
		pkgPath, _, ok := cutLast(attrType, ".")
		if ok {
			// For "type=foo.Name", we need to ensure that "foo" is imported.
			g.importedAs[pkgPath] = pkgPath
			// For "type=foo/bar.Name", the selector is just "bar.Name".
			// Note that this doesn't support Go packages whose name does not match
			// the last element of their import path. That seems OK for now.
			_, attrType, _ = cutLast(attrType, "/")
		}
		g.appendf("%s", attrType)
		return nil
	}
	// TODO: support nullable types, such as `null | #SomeReference` and
	// `null | {foo: int}`.
	if g.emitTypeReference(val, optional) {
		return nil
	}

	switch k := val.IncompleteKind(); k {
	case cue.StructKind:
		if elem := val.LookupPath(cue.MakePath(cue.AnyString)); elem.Err() == nil {
			g.appendf("map[string]")
			if err := g.emitType(elem, false); err != nil {
				return err
			}
			break
		}
		// A disjunction of structs cannot be represented in Go, as it does not have sum types.
		// Fall back to a map of string to any, which is not ideal, but will work for any field.
		//
		// TODO: consider alternatives, such as:
		// * For `#StructFoo | #StructBar`, generate named types for each disjunct,
		//   and use `any` here as a sum type between them.
		// * For a disjunction of closed structs, generate a flat struct with the superset
		//   of all fields, akin to a C union.
		if op, _ := val.Expr(); op == cue.OrOp {
			g.appendf("map[string]any")
			break
		}
		// TODO: treat a single embedding like `{[string]: int}` like we would `[string]: int`
		if optional {
			g.appendf("*")
		}
		g.appendf("struct {\n")
		iter, err := val.Fields(cue.Definitions(true), cue.Optional(true))
		if err != nil {
			return err
		}
		for iter.Next() {
			sel := iter.Selector()
			val := iter.Value()
			if sel.IsDefinition() {
				// TODO: why does removing [cue.Definitions] above break the tests?
				continue
			}
			cueName := sel.String()
			cueName = strings.TrimRight(cueName, "?!")
			g.emitDocs(cueName, val.Doc())
			// TODO: should we ensure that optional fields are always nilable in Go?
			// On one hand this allows telling int64(0) apart from a missing field,
			// but on the other, it's often unnecessary and leads to clumsy types.
			// Perhaps add a @go() attribute parameter to require nullability.
			//
			// For now, only structs are always pointers when optional.
			// This is necessary to allow recursive Go types such as linked lists.
			// Pointers to structs are still OK in terms of UX, given that
			// one can do X.PtrY.Z without needing to do (*X.PtrY).Z.
			optional := sel.ConstraintType()&cue.OptionalConstraint != 0

			// We want the Go name from just this selector, even when it's not a definition.
			goName := goNameFromPath(cue.MakePath(sel), false)

			goAttr := val.Attribute("go")
			if s, _ := goAttr.String(0); s != "" {
				goName = s
			}

			g.appendf("%s ", goName)
			if err := g.emitType(val, optional); err != nil {
				return err
			}
			// TODO: should we generate cuego tags like `cue:"expr"`?
			// If not, at least move the /* CUE */ comments to the end of the line.
			omitEmpty := ""
			if optional {
				omitEmpty = ",omitempty"
			}
			g.appendf(" `json:\"%s%s\"`", cueName, omitEmpty)
			g.appendf("\n\n")
		}
		g.appendf("}")
	case cue.ListKind:
		// We mainly care about patterns like [...string].
		// Anything else can convert into []any as a fallback.
		g.appendf("[]")
		elem := val.LookupPath(cue.MakePath(cue.AnyIndex))
		if !elem.Exists() {
			// TODO: perhaps mention the original type.
			g.appendf("any /* CUE closed list */")
		} else if err := g.emitType(elem, false); err != nil {
			return err
		}

	case cue.NullKind:
		g.appendf("*struct{} /* CUE null */")
	case cue.BoolKind:
		g.appendf("bool")
	case cue.IntKind:
		g.appendf("int64")
	case cue.FloatKind:
		g.appendf("float64")
	case cue.StringKind:
		g.appendf("string")
	case cue.BytesKind:
		g.appendf("[]byte")

	case cue.NumberKind:
		// Can we do better for numbers?
		g.appendf("any /* CUE number; int64 or float64 */")

	case cue.TopKind:
		g.appendf("any /* CUE top */")

	// TODO: generate e.g. int8 where appropriate
	// TODO: uint64 would be marginally better than int64 for unsigned integer types

	default:
		// A disjunction of various kinds cannot be represented in Go, as it does not have sum types.
		// Also see the potential approaches in the TODO about disjunctions of structs.
		if op, _ := val.Expr(); op == cue.OrOp {
			g.appendf("any /* CUE disjunction: %s */", k)
			break
		}
		g.appendf("any /* TODO: IncompleteKind: %s */", k)
	}
	return nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return "", s, false
}

// goNameFromPath transforms a CUE path, such as "#foo.bar?",
// into a suitable name for a generated Go type, such as "Foo_bar".
// When defsOnly is true, all path elements must be definitions, or "" is returned.
func goNameFromPath(path cue.Path, defsOnly bool) string {
	export := true
	var sb strings.Builder
	for i, sel := range path.Selectors() {
		if defsOnly && !sel.IsDefinition() {
			return ""
		}
		if i > 0 {
			// To aid in readability, nested names are separated with underscores.
			sb.WriteString("_")
		}
		str := sel.String()
		str, hidden := strings.CutPrefix(str, "_")
		if hidden {
			// If any part of the path is hidden, we are not exporting.
			// TODO: we currently don't generate hidden definitions unless XXX
			export = false
		}
		// Leading or trailing characters for definitions, optional, or required
		// are not included as part of Go names.
		str = strings.TrimPrefix(str, "#")
		str = strings.TrimRight(str, "?!")
		sb.WriteString(str)
	}
	name := sb.String()
	if export {
		name = strings.Title(name)
	}
	// TODO: lowercase if not exporting
	return name
}

// goPkgNameForInstance determines what to name a Go package generated from a CUE instance.
// By default this is the CUE package name, but it can be overriden by a @go() package attribute.
func goPkgNameForInstance(inst *build.Instance, instVal cue.Value) string {
	attrs := instVal.Attributes(cue.DeclAttr)
	for _, attr := range attrs {
		if attr.Name() == "go" {
			if s, _ := attr.String(0); s != "" {
				return s
			}
			break
		}
	}
	return inst.PkgName
}

// emitTypeReference attempts to generate a CUE value as a Go type via a reference,
// either to a type in the same Go package, or to a type in an imported package.
func (g *generator) emitTypeReference(val cue.Value, optional bool) bool {
	// References to existing names, either from the same package or an imported package.
	root, path := val.ReferencePath()
	// TODO: surely there is a better way to check whether ReferencePath returned "no path",
	// such as a possible path.IsValid method?
	if len(path.Selectors()) == 0 {
		return false
	}
	inst := root.BuildInstance()
	// Go has no notion of qualified import paths; if a CUE file imports
	// "foo.com/bar:qualified", we import just "foo.com/bar" on the Go side.
	// TODO: deal with multiple packages existing in the same directory.
	unqualifiedPath := module.ParseImportPath(inst.ImportPath).Unqualified().String()

	var sb strings.Builder
	if optional && cue.Dereference(val).IncompleteKind() == cue.StructKind {
		sb.WriteString("*")
	}
	if root != g.pkgRoot {
		sb.WriteString(goPkgNameForInstance(inst, root))
		sb.WriteString(".")
	}

	// As a special case, some CUE standard library types are allowed as references
	// even though they aren't definitions.
	defsOnly := true
	switch fmt.Sprintf("%s.%s", unqualifiedPath, path) {
	case "time.Duration":
		// Note that CUE represents durations as strings, but Go as int64.
		// TODO: can we do better here, such as a custom duration type?
		g.appendf("string /* CUE time.Duration */")
		return true
	case "time.Time":
		defsOnly = false
	}

	name := goNameFromPath(path, defsOnly)
	if name == "" {
		return false // Not a path we are generating.
	}

	sb.WriteString(name)
	g.appendf("%s", sb.String())

	// We did use a reference; if the referenced name was from another package,
	// we need to ensure that package is imported.
	// Otherwise, we need to ensure that the referenced local definition is generated.
	if root != g.pkgRoot {
		g.importedAs[inst.ImportPath] = unqualifiedPath
	} else {
		g.addDef(path)
	}
	return true
}

// emitDocs generates the documentation comments attached to the following declaration.
func (g *generator) emitDocs(name string, groups []*ast.CommentGroup) {
	// TODO: place the comment group starting with `// $name ...` first.
	// TODO: ensure that the Go name is used in the godoc.
	for i, group := range groups {
		if i > 0 {
			g.appendf("//\n")
		}
		for _, line := range group.List {
			g.appendf("%s\n", line.Text)
		}
	}
}
