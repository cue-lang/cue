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
	"strconv"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/encoding"
)

// This file contains logic for placing orphan files within a CUE namespace.

func (b *buildPlan) usePlacement() bool {
	return b.perFile || b.useList || len(b.path) > 0
}

func (b *buildPlan) parsePlacementFlags() error {
	cmd := b.cmd
	b.perFile = flagFiles.Bool(cmd)
	b.useList = flagList.Bool(cmd)
	b.useContext = flagWithContext.Bool(cmd)

	for _, str := range flagPath.StringArray(cmd) {
		l, err := parser.ParseExpr("--path", str)
		if err != nil {
			labels, err := parseFullPath(str)
			if err != nil {
				return fmt.Errorf(
					`labels must be expressions (-l foo -l 'strings.ToLower(bar)') or full paths (-l '"foo": "\(strings.ToLower(bar))":) : %v`, err)
			}
			b.path = append(b.path, labels...)
			continue
		}

		b.path = append(b.path, &ast.ParenExpr{X: l})
	}

	if !b.importing && !b.perFile && !b.useList && len(b.path) == 0 {
		if b.useContext {
			return fmt.Errorf(
				"flag %q must be used with at least one of flag %q, %q, or %q",
				flagWithContext, flagPath, flagList, flagFiles,
			)
		}
	} else if b.schema != nil {
		return fmt.Errorf(
			"cannot combine --%s flag with flag %q, %q, or %q",
			flagSchema, flagPath, flagList, flagFiles,
		)
	}
	return nil
}

func (b *buildPlan) placeOrphans(i *build.Instance, a []*decoderInfo) error {
	pkg := b.encConfig.PkgName
	if pkg == "" {
		pkg = i.PkgName
	} else if pkg != "" && i.PkgName != "" && i.PkgName != pkg && !flagForce.Bool(b.cmd) {
		return fmt.Errorf(
			"%q flag clashes with existing package name (%s vs %s)",
			flagPackage, pkg, i.PkgName,
		)
	}

	var files []*ast.File

	for _, di := range a {
		if !i.User && !b.matchFile(filepath.Base(di.file.Filename)) {
			continue
		}

		d := di.dec(b)

		var objs []*ast.File

		// Filter only need to filter files that can stream:
		for ; !d.Done(); d.Next() {
			if f := d.File(); f != nil {
				f.Filename = newName(d.Filename(), 0)
				objs = append(objs, f)
			}
		}
		if err := d.Err(); err != nil {
			return err
		}

		if b.perFile {
			for i, obj := range objs {
				f, err := placeOrphans(b, d, pkg, obj)
				if err != nil {
					return err
				}
				f.Filename = newName(d.Filename(), i)
				files = append(files, f)
			}
			continue
		}
		// TODO: consider getting rid of this requirement. It is important that
		// import will catch conflicts ahead of time then, though, and report
		// this messages as a possible solution if there are conflicts.
		if b.importing && len(objs) > 1 && len(b.path) == 0 && !b.useList {
			return fmt.Errorf(
				"%s, %s, or %s flag needed to handle multiple objects in file %s",
				flagPath, flagList, flagFiles, shortFile(i.Root, di.file))
		}

		if !b.useList && len(b.path) == 0 && !b.useContext {
			for _, f := range objs {
				if pkg := b.encConfig.PkgName; pkg != "" {
					internal.SetPackage(f, pkg, false)
				}
				files = append(files, f)
			}
		} else {
			// TODO: handle imports correctly, i.e. for proto.
			f, err := placeOrphans(b, d, pkg, objs...)
			if err != nil {
				return err
			}
			f.Filename = newName(d.Filename(), 0)
			files = append(files, f)
		}
	}

	b.imported = append(b.imported, files...)
	for _, f := range files {
		if err := i.AddSyntax(f); err != nil {
			return err
		}
	}
	return nil
}

func placeOrphans(b *buildPlan, d *encoding.Decoder, pkg string, objs ...*ast.File) (*ast.File, error) {
	f := &ast.File{}
	filename := d.Filename()

	index := newIndex()
	for i, file := range objs {
		if i == 0 {
			astutil.CopyMeta(f, file)
		}
		expr := internal.ToExpr(file)
		p, _, _ := internal.PackageInfo(file)

		var path cue.Path
		var labels []ast.Label

		switch {
		case len(b.path) > 0:
			expr := expr
			if b.useContext {
				expr = ast.NewStruct(
					"data", expr,
					"filename", ast.NewString(filename),
					"index", ast.NewLit(token.INT, strconv.Itoa(i)),
					"recordCount", ast.NewLit(token.INT, strconv.Itoa(len(objs))),
				)
			}
			var f *ast.File
			if s, ok := expr.(*ast.StructLit); ok {
				f = &ast.File{Decls: s.Elts}
			} else {
				f = &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: expr}}}
			}
			err := astutil.Sanitize(f)
			if err != nil {
				return nil, errors.Wrapf(err, token.NoPos,
					"invalid combination of input files")
			}
			inst, err := runtime.CompileFile(f)
			if err != nil {
				return nil, err
			}

			var a []cue.Selector

			for _, label := range b.path {
				switch x := label.(type) {
				case *ast.Ident, *ast.BasicLit:
				case ast.Expr:
					if p, ok := x.(*ast.ParenExpr); ok {
						x = p.X // unwrap for better error messages
					}
					switch l := inst.Eval(x); l.Kind() {
					case cue.StringKind, cue.IntKind:
						label = l.Syntax().(ast.Label)

					default:
						var arg interface{} = l
						if err := l.Err(); err != nil {
							arg = err
						}
						return nil, fmt.Errorf(
							`error evaluating label %v: %v`,
							astinternal.DebugStr(x), arg)
					}
				}
				ast.SetPos(label, token.NoPos)
				a = append(a, cue.Label(label))
				labels = append(labels, label)
			}

			path = cue.MakePath(a...)
		}

		switch d.Interpretation() {
		case build.ProtobufJSON:
			v := b.instance.Value().LookupPath(path)
			if b.useList {
				v, _ = v.Elem()
			}
			if !v.Exists() {
				break
			}
			if err := jsonpb.NewDecoder(v).RewriteFile(file); err != nil {
				return nil, err
			}
		}

		if b.useList {
			idx := index
			for _, e := range labels {
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
		} else if len(labels) == 0 {
			obj, ok := expr.(*ast.StructLit)
			if !ok {
				if _, ok := expr.(*ast.ListLit); ok {
					return nil, fmt.Errorf("expected struct as object root, did you mean to use the --list flag?")
				}
				return nil, fmt.Errorf("cannot map non-struct to object root")
			}
			f.Decls = append(f.Decls, obj.Elts...)
		} else {
			field := &ast.Field{Label: labels[0]}
			f.Decls = append(f.Decls, field)
			if p != nil {
				astutil.CopyComments(field, p)
			}
			for _, e := range labels[1:] {
				newField := &ast.Field{Label: e}
				newVal := ast.NewStruct(newField)
				field.Value = newVal
				field = newField
			}
			field.Value = expr
		}
	}

	if pkg != "" {
		internal.SetPackage(f, pkg, false)
	}

	if b.useList {
		switch x := index.field.Value.(type) {
		case *ast.StructLit:
			f.Decls = append(f.Decls, x.Elts...)
		case *ast.ListLit:
			f.Decls = append(f.Decls, &ast.EmbedDecl{Expr: x})
		default:
			panic("unreachable")
		}
	}

	return f, astutil.Sanitize(f)
}

func parseFullPath(exprs string) (p []ast.Label, err error) {
	f, err := parser.ParseFile("--path", exprs+"_")
	if err != nil {
		return p, fmt.Errorf("parser error in path %q: %v", exprs, err)
	}

	if len(f.Decls) != 1 {
		return p, errors.New("path flag must be a space-separated sequence of labels")
	}

	for d := f.Decls[0]; ; {
		field, ok := d.(*ast.Field)
		if !ok {
			// This should never happen
			return p, errors.New("%q not a sequence of labels")
		}

		switch x := field.Label.(type) {
		case *ast.Ident, *ast.BasicLit:
			p = append(p, x)

		case ast.Expr:
			p = append(p, &ast.ParenExpr{X: x})

		default:
			return p, fmt.Errorf("unsupported label type %T", x)
		}

		v, ok := field.Value.(*ast.StructLit)
		if !ok {
			break
		}

		if len(v.Elts) != 1 {
			return p, errors.New("path value may not contain a struct")
		}

		d = v.Elts[0]
	}
	return p, nil
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
	key := astinternal.DebugStr(label)
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
	if filename == "-" {
		return filename
	}
	ext := filepath.Ext(filename)
	filename = filename[:len(filename)-len(ext)]
	if i > 0 {
		filename += fmt.Sprintf("-%d", i)
	}
	filename += ".cue"
	return filename
}
