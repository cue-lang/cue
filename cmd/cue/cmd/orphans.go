// Copyright 2020 CUE Authors
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

package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/encoding"
)

// This file contains logic for placing orphan files within a CUE namespace.

func (b *buildPlan) placeOrphans(i *build.Instance) (ok bool, err error) {
	var (
		perFile    = flagFiles.Bool(b.cmd)
		useList    = flagList.Bool(b.cmd)
		path       = flagPath.StringArray(b.cmd)
		useContext = flagWithContext.Bool(b.cmd)
		pkg        = flagPackage.String(b.cmd)
		match      = flagGlob.String(b.cmd)
	)
	if !b.forceOrphanProcessing && !perFile && !useList && len(path) == 0 {
		if useContext {
			return false, fmt.Errorf(
				"flag %q must be used with at least one of flag %q, %q, or %q",
				flagWithContext, flagPath, flagList, flagFiles,
			)
		}
		return false, err
	}

	if pkg == "" {
		pkg = i.PkgName
	} else if pkg != "" && i.PkgName != "" && i.PkgName != pkg && !flagForce.Bool(b.cmd) {
		return false, fmt.Errorf(
			"%q flag clashes with existing package name (%s vs %s)",
			flagPackage, pkg, i.PkgName,
		)
	}

	var files []*ast.File

	re, err := regexp.Compile(match)
	if err != nil {
		return false, err
	}

	for _, f := range i.OrphanedFiles {
		if !re.MatchString(filepath.Base(f.Filename)) {
			return false, nil
		}

		d := encoding.NewDecoder(f, b.encConfig)
		defer d.Close()

		var objs []ast.Expr

		for ; !d.Done(); d.Next() {
			if expr := d.Expr(); expr != nil {
				objs = append(objs, expr)
				continue
			}
			f := d.File()
			f.Filename = newName(d.Filename(), d.Index())
			files = append(files, f)
		}

		if perFile {
			for i, obj := range objs {
				f, err := placeOrphans(b.cmd, d.Filename(), pkg, obj)
				if err != nil {
					return false, err
				}
				f.Filename = newName(d.Filename(), i)
				files = append(files, f)
			}
			continue
		}
		if len(objs) > 1 && len(path) == 0 && useList {
			return false, fmt.Errorf(
				"%s, %s, or %s flag needed to handle multiple objects in file %s",
				flagPath, flagList, flagFiles, f.Filename)
		}

		f, err := placeOrphans(b.cmd, d.Filename(), pkg, objs...)
		if err != nil {
			return false, err
		}
		f.Filename = newName(d.Filename(), 0)
		files = append(files, f)
	}

	b.imported = append(b.imported, files...)
	for _, f := range files {
		if err := i.AddSyntax(f); err != nil {
			return false, err
		}
		i.BuildFiles = append(i.BuildFiles, &build.File{
			Filename: f.Filename,
			Encoding: build.CUE,
			Source:   f,
		})
	}
	return true, nil
}

func placeOrphans(cmd *Command, filename, pkg string, objs ...ast.Expr) (*ast.File, error) {
	f := &ast.File{}

	index := newIndex()
	for i, expr := range objs {

		// Compute a path different from root.
		var pathElems []ast.Label

		switch {
		case len(flagPath.StringArray(cmd)) > 0:
			expr := expr
			if flagWithContext.Bool(cmd) {
				expr = ast.NewStruct(
					"data", expr,
					"filename", ast.NewString(filename),
					"index", ast.NewLit(token.INT, strconv.Itoa(i)),
					"recordCount", ast.NewLit(token.INT, strconv.Itoa(len(objs))),
				)
			}
			inst, err := runtime.CompileExpr(expr)
			if err != nil {
				return nil, err
			}

			for _, str := range flagPath.StringArray(cmd) {
				l, err := parser.ParseExpr("--path", str)
				if err != nil {
					return nil, fmt.Errorf(`labels are of form "cue import -l foo -l 'strings.ToLower(bar)'": %v`, err)
				}

				str, err := inst.Eval(l).String()
				if err != nil {
					return nil, fmt.Errorf("unsupported label path type: %v", err)
				}
				pathElems = append(pathElems, ast.NewString(str))
			}
		}

		if flagList.Bool(cmd) {
			idx := index
			for _, e := range pathElems {
				idx = idx.label(e)
			}
			if idx.field.Value == nil {
				idx.field.Value = &ast.ListLit{
					Lbrack: token.NoSpace.Pos(),
					Rbrack: token.NoSpace.Pos(),
				}
			}
			list := idx.field.Value.(*ast.ListLit)
			list.Elts = append(list.Elts, expr)
		} else if len(pathElems) == 0 {
			obj, ok := expr.(*ast.StructLit)
			if !ok {
				if _, ok := expr.(*ast.ListLit); ok {
					return nil, fmt.Errorf("expected struct as object root, did you mean to use the --list flag?")
				}
				return nil, fmt.Errorf("cannot map non-struct to object root")
			}
			f.Decls = append(f.Decls, obj.Elts...)
		} else {
			field := &ast.Field{Label: pathElems[0]}
			f.Decls = append(f.Decls, field)
			for _, e := range pathElems[1:] {
				newField := &ast.Field{Label: e}
				newVal := ast.NewStruct(newField)
				field.Value = newVal
				field = newField
			}
			field.Value = expr
		}
	}

	if pkg != "" {
		p := &ast.Package{Name: ast.NewIdent(pkg)}
		f.Decls = append([]ast.Decl{p}, f.Decls...)
	}

	if flagList.Bool(cmd) {
		switch x := index.field.Value.(type) {
		case *ast.StructLit:
			f.Decls = append(f.Decls, x.Elts...)
		case *ast.ListLit:
			f.Decls = append(f.Decls, &ast.EmbedDecl{Expr: x})
		default:
			panic("unreachable")
		}
	}

	return f, nil
}

type listIndex struct {
	index map[string]*listIndex
	field *ast.Field
}

func newIndex() *listIndex {
	return &listIndex{
		index: map[string]*listIndex{},
		field: &ast.Field{},
	}
}

func (x *listIndex) label(label ast.Label) *listIndex {
	key := internal.DebugStr(label)
	idx := x.index[key]
	if idx == nil {
		if x.field.Value == nil {
			x.field.Value = &ast.StructLit{}
		}
		obj := x.field.Value.(*ast.StructLit)
		newField := &ast.Field{Label: label}
		obj.Elts = append(obj.Elts, newField)
		idx = &listIndex{
			index: map[string]*listIndex{},
			field: newField,
		}
		x.index[key] = idx
	}
	return idx
}

func newName(filename string, i int) string {
	ext := filepath.Ext(filename)
	filename = filename[:len(filename)-len(ext)]
	if i > 0 {
		filename += fmt.Sprintf("-%d", i)
	}
	filename += ".cue"
	return filename
}
