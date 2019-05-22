// Copyright 2019 CUE Authors
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

package protobuf

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"github.com/emicklei/proto"
	"golang.org/x/xerrors"
)

type sharedState struct {
	paths []string
}

func (s *sharedState) parse(filename string, r io.Reader) (p *protoConverter, err error) {
	// Determine files to convert.
	if r == nil {
		f, err := os.Open(filename)
		if err != nil {
			return nil, xerrors.Errorf("protobuf: %w", err)
		}
		defer f.Close()
		r = f
	}

	parser := proto.NewParser(r)
	if filename != "" {
		parser.Filename(filename)
	}
	d, err := parser.Parse()
	if err != nil {
		return nil, xerrors.Errorf("protobuf: %w", err)
	}

	p = &protoConverter{
		state:   s,
		used:    map[string]bool{},
		symbols: map[string]bool{},
	}

	defer func() {
		switch x := recover().(type) {
		case nil:
		case protoError:
			err = &Error{
				Filename: filename,
				Path:     strings.Join(p.path, "."),
				Err:      x.error,
			}
		default:
			panic(x)
		}
	}()

	p.file = &ast.File{Filename: filename}

	p.addNames(d.Elements)

	// Parse package definitions.
	for _, e := range d.Elements {
		switch x := e.(type) {
		case *proto.Package:
			p.protoPkg = x.Name
		case *proto.Option:
			if x.Name == "go_package" {
				str, err := strconv.Unquote(x.Constant.SourceRepresentation())
				if err != nil {
					failf("unquoting package filed: %v", err)
				}
				split := strings.Split(str, ";")
				p.goPkgPath = split[0]
				switch len(split) {
				case 1:
					p.goPkg = path.Base(str)
				case 2:
					p.goPkg = split[1]
				default:
					failf("unexpected ';' in %q", str)
				}
				p.file.Name = ast.NewIdent(p.goPkg)
				// name.AddComment(comment(x.Comment, true))
				// name.AddComment(comment(x.InlineComment, false))
			}
		}
	}

	for _, e := range d.Elements {
		switch x := e.(type) {
		case *proto.Import:
			p.doImport(x)
		}
	}

	imports := &ast.ImportDecl{}
	p.file.Decls = append(p.file.Decls, imports)

	for _, e := range d.Elements {
		p.topElement(e)
	}

	used := []string{}
	for k := range p.used {
		used = append(used, k)
	}
	sort.Strings(used)

	for _, v := range used {
		imports.Specs = append(imports.Specs, &ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(v)},
		})
	}

	if len(imports.Specs) == 0 {
		p.file.Decls = p.file.Decls[1:]
	}

	return p, nil
}

// A protoConverter converts a proto definition to CUE. Proto files map to
// CUE files one to one.
type protoConverter struct {
	state *sharedState

	proto3 bool

	protoPkg  string
	goPkg     string
	goPkgPath string

	// w bytes.Buffer
	file   *ast.File
	inBody bool

	imports map[string]string
	used    map[string]bool

	path    []string
	scope   []map[string]mapping // for symbols resolution within package.
	symbols map[string]bool      // symbols provided by package
}

type mapping struct {
	ref string
	pkg *protoConverter
}

type pkgInfo struct {
	importPath string // the import path
	goPath     string // The Go import path
	shortName  string // Used for the cue package path, default is base of goPath
}

func (p *protoConverter) addRef(from, to string) {
	top := p.scope[len(p.scope)-1]
	if _, ok := top[from]; ok {
		failf("entity %q already defined", from)
	}
	top[from] = mapping{ref: to}
}

func (p *protoConverter) addNames(elems []proto.Visitee) {
	p.scope = append(p.scope, map[string]mapping{})
	for _, e := range elems {
		var name string
		switch x := e.(type) {
		case *proto.Message:
			if x.IsExtend {
				continue
			}
			name = x.Name
		case *proto.Enum:
			name = x.Name
		default:
			continue
		}
		sym := strings.Join(append(p.path, name), ".")
		p.symbols[sym] = true
		p.addRef(name, strings.Join(append(p.path, name), "_"))
	}
}

func (p *protoConverter) popNames() {
	p.scope = p.scope[:len(p.scope)-1]
}

func (p *protoConverter) resolve(name string, options []*proto.Option) string {
	if strings.HasPrefix(name, ".") {
		return p.resolveTopScope(name[1:], options)
	}
	for i := len(p.scope) - 1; i > 0; i-- {
		if m, ok := p.scope[i][name]; ok {
			return m.ref
		}
	}
	return p.resolveTopScope(name, options)
}

func (p *protoConverter) resolveTopScope(name string, options []*proto.Option) string {
	for i := 0; i < len(name); i++ {
		k := strings.IndexByte(name[i:], '.')
		i += k
		if k == -1 {
			i = len(name)
		}
		if m, ok := p.scope[0][name[:i]]; ok {
			if m.pkg != nil {
				p.used[m.pkg.goPkgPath] = true
			}
			return m.ref + name[i:]
		}
	}
	if s, ok := protoToCUE(name, options); ok {
		return s
	}
	failf("name %q not found", name)
	return ""
}

func (p *protoConverter) doImport(v *proto.Import) {
	if v.Filename == "cuelang/cue.proto" {
		return
	}

	filename := ""
	for _, p := range p.state.paths {
		name := filepath.Join(p, v.Filename)
		_, err := os.Stat(name)
		if err != nil {
			continue
		}
		filename = name
		break
	}

	if filename == "" {
		p.mustBuiltinPackage(v.Filename)
		return
	}

	imp, err := p.state.parse(filename, nil)
	if err != nil {
		fail(err)
	}

	prefix := ""
	if imp.goPkgPath != p.goPkgPath {
		prefix = imp.goPkg + "."
	}

	pkgNamespace := strings.Split(imp.protoPkg, ".")
	curNamespace := strings.Split(p.protoPkg, ".")
	for {
		for k := range imp.symbols {
			ref := k
			if len(pkgNamespace) > 0 {
				ref = strings.Join(append(pkgNamespace, k), ".")
			}
			if _, ok := p.scope[0][ref]; !ok {
				pkg := imp
				if imp.goPkgPath == p.goPkgPath {
					pkg = nil
				}
				p.scope[0][ref] = mapping{prefix + k, pkg}
			}
		}
		if len(pkgNamespace) == 0 {
			break
		}
		if len(curNamespace) == 0 || pkgNamespace[0] != curNamespace[0] {
			break
		}
		pkgNamespace = pkgNamespace[1:]
		curNamespace = curNamespace[1:]
	}
}

func (p *protoConverter) stringLit(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

func (p *protoConverter) ref() *ast.Ident {
	return ast.NewIdent(strings.Join(p.path, "_"))
}

func (p *protoConverter) subref(name string) *ast.Ident {
	return ast.NewIdent(strings.Join(append(p.path, name), "_"))
}

func (p *protoConverter) addTag(f *ast.Field, body string) {
	tag := "@protobuf(" + body + ")"
	f.Attrs = append(f.Attrs, &ast.Attribute{Text: tag})
}

func (p *protoConverter) topElement(v proto.Visitee) {
	switch x := v.(type) {
	case *proto.Syntax:
		p.proto3 = x.Value == "proto3"

	case *proto.Comment:
		if p.inBody {
			p.file.Decls = append(p.file.Decls, comment(x, true))
		} else {
			addComments(p.file, 0, x, nil)
		}

	case *proto.Enum:
		p.enum(x)

	case *proto.Package:
		if doc := x.Doc(); doc != nil {
			addComments(p.file, 0, doc, nil)
		}
		// p.inBody bool

	case *proto.Message:
		p.message(x)

	case *proto.Option:
	case *proto.Import:
		// already handled.

	default:
		failf("unsupported type %T", x)
	}
}

func (p *protoConverter) message(v *proto.Message) {
	defer func(saved []string) { p.path = saved }(p.path)
	p.path = append(p.path, v.Name)

	p.addNames(v.Elements)
	defer p.popNames()

	// TODO: handle IsExtend/ proto2

	s := &ast.StructLit{
		// TOOD: set proto file position.
	}

	ref := p.ref()
	if v.Comment == nil {
		ref.NamePos = newSection
	}
	f := &ast.Field{Label: ref, Value: s}
	addComments(f, 1, v.Comment, nil)

	// In CUE a message is always defined at the top level.
	p.file.Decls = append(p.file.Decls, f)

	for i, e := range v.Elements {
		p.messageField(s, i, e)
	}
}

func (p *protoConverter) messageField(s *ast.StructLit, i int, v proto.Visitee) {
	switch x := v.(type) {
	case *proto.Comment:
		s.Elts = append(s.Elts, comment(x, true))

	case *proto.NormalField:
		f := p.parseField(s, i, x.Field)

		if x.Repeated {
			f.Value = &ast.ListLit{
				Ellipsis: token.NoSpace.Pos(),
				Type:     f.Value,
			}
		}

	case *proto.MapField:
		f := &ast.Field{}

		// All keys are converted to strings.
		// TODO: support integer keys.
		f.Label = &ast.TemplateLabel{Ident: ast.NewIdent("_")}
		f.Value = ast.NewIdent(p.resolve(x.Type, x.Options))

		name := labelName(x.Name)
		f = &ast.Field{
			Label: ast.NewIdent(name),
			Value: &ast.StructLit{Elts: []ast.Decl{f}},
		}
		addComments(f, i, x.Comment, x.InlineComment)

		o := optionParser{message: s, field: f}
		o.tags = fmt.Sprintf("%d,type=map<%s,%s>", x.Sequence, x.KeyType, x.Type)
		if x.Name != name {
			o.tags += "," + x.Name
		}
		s.Elts = append(s.Elts, f)
		o.parse(x.Options)
		p.addTag(f, o.tags)

	case *proto.Enum:
		p.enum(x)

	case *proto.Message:
		p.message(x)

	case *proto.Oneof:
		p.oneOf(x)

	default:
		failf("unsupported type %T", v)
	}
}

// enum converts a proto enum definition to CUE.
//
// An enum will generate two top-level definitions:
//
//    Enum:
//      "Value1" |
//      "Value2" |
//      "Value3"
//
// and
//
//    Enum_value: {
//        "Value1": 0
//        "Value2": 1
//    }
//
// Enums are always defined at the top level. The name of a nested enum
// will be prefixed with the name of its parent and an underscore.
func (p *protoConverter) enum(x *proto.Enum) {
	if len(x.Elements) == 0 {
		failf("empty enum")
	}

	name := p.subref(x.Name)

	p.addNames(x.Elements)

	if len(p.path) == 0 {
		defer func() { p.path = p.path[:0] }()
		p.path = append(p.path, x.Name)
	}

	// Top-level enum entry.
	enum := &ast.Field{Label: name}
	addComments(enum, 1, x.Comment, nil)

	// Top-level enum values entry.
	valueName := ast.NewIdent(name.Name + "_value")
	valueName.NamePos = newSection
	valueMap := &ast.StructLit{}
	d := &ast.Field{Label: valueName, Value: valueMap}
	// addComments(valueMap, 1, x.Comment, nil)

	p.file.Decls = append(p.file.Decls, enum, d)

	// The line comments for an enum field need to attach after the '|', which
	// is only known at the next iteration.
	var lastComment *proto.Comment
	for i, v := range x.Elements {
		switch y := v.(type) {
		case *proto.EnumField:
			// Add enum value to map
			f := &ast.Field{
				Label: p.stringLit(y.Name),
				Value: &ast.BasicLit{Value: strconv.Itoa(y.Integer)},
			}
			valueMap.Elts = append(valueMap.Elts, f)

			// add to enum disjunction
			value := p.stringLit(y.Name)

			var e ast.Expr = value
			// Make the first value the default value.
			if i == 0 {
				e = &ast.UnaryExpr{OpPos: newline, Op: token.MUL, X: value}
			} else {
				value.ValuePos = newline
			}
			addComments(e, i, y.Comment, nil)
			if enum.Value != nil {
				e = &ast.BinaryExpr{X: enum.Value, Op: token.OR, Y: e}
				if cg := comment(lastComment, false); cg != nil {
					cg.Position = 2
					e.AddComment(cg)
				}
			}
			enum.Value = e

			if y.Comment != nil {
				lastComment = nil
				addComments(f, 0, nil, y.InlineComment)
			} else {
				lastComment = y.InlineComment
			}

			// a := fmt.Sprintf("@protobuf(enum,name=%s)", y.Name)
			// f.Attrs = append(f.Attrs, &ast.Attribute{Text: a})
		}
	}
	addComments(enum.Value, 1, nil, lastComment)
}

func (p *protoConverter) oneOf(x *proto.Oneof) {
	f := &ast.Field{
		Label: p.ref(),
	}
	f.AddComment(comment(x.Comment, true))

	p.file.Decls = append(p.file.Decls, f)

	for _, v := range x.Elements {
		s := &ast.StructLit{}
		switch x := v.(type) {
		case *proto.OneOfField:
			f := p.parseField(s, 0, x.Field)
			f.Optional = token.NoPos

		default:
			p.messageField(s, 1, v)
		}
		var e ast.Expr = s
		if f.Value != nil {
			e = &ast.BinaryExpr{X: f.Value, Op: token.OR, Y: s}
		}
		f.Value = e
	}
}

func (p *protoConverter) parseField(s *ast.StructLit, i int, x *proto.Field) *ast.Field {
	f := &ast.Field{}
	addComments(f, i, x.Comment, x.InlineComment)

	name := labelName(x.Name)
	f.Label = ast.NewIdent(name)
	typ := p.resolve(x.Type, x.Options)
	f.Value = ast.NewIdent(typ)
	s.Elts = append(s.Elts, f)

	o := optionParser{message: s, field: f}

	// body of @protobuf tag: sequence[,type][,name=<name>][,...]
	o.tags += fmt.Sprint(x.Sequence)
	if x.Type != typ {
		o.tags += ",type=" + x.Type
	}
	if x.Name != name {
		o.tags += ",name=" + x.Name
	}
	o.parse(x.Options)
	p.addTag(f, o.tags)

	if !o.required {
		f.Optional = token.NoSpace.Pos()
	}
	return f
}

type optionParser struct {
	message  *ast.StructLit
	field    *ast.Field
	required bool
	tags     string
}

func (p *optionParser) parse(options []*proto.Option) {

	// TODO: handle options
	// - translate options to tags
	// - interpret CUE options.
	for _, o := range options {
		switch o.Name {
		case "(cue_opt).required":
			p.required = true
			// TODO: Dropping comments. Maybe add a dummy tag?

		case "(cue.val)":
			// TODO: set filename and base offset.
			expr, err := parser.ParseExpr("", o.Constant.Source)
			if err != nil {
				failf("invalid cue.val value: %v", err)
			}
			// Any further checks will be done at the end.
			constraint := &ast.Field{Label: p.field.Label, Value: expr}
			addComments(constraint, 1, o.Comment, o.InlineComment)
			p.message.Elts = append(p.message.Elts, constraint)
			if !p.required {
				constraint.Optional = token.NoSpace.Pos()
			}

		default:
			// TODO: dropping comments. Maybe add dummy tag?

			// TODO: should CUE support nested attributes?
			source := o.Constant.SourceRepresentation()
			p.tags += "," + quote("option("+o.Name+","+source+")")
		}
	}
}
