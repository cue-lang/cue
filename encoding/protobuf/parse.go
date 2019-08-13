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
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/scanner"
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/source"
	"github.com/emicklei/proto"
)

func (s *Extractor) parse(filename string, src interface{}) (p *protoConverter, err error) {
	if filename == "" {
		return nil, errors.Newf(token.NoPos, "empty filename")
	}
	if r, ok := s.fileCache[filename]; ok {
		return r.p, r.err
	}
	defer func() {
		s.fileCache[filename] = result{p, err}
	}()

	b, err := source.Read(filename, src)
	if err != nil {
		return nil, err
	}

	parser := proto.NewParser(bytes.NewReader(b))
	if filename != "" {
		parser.Filename(filename)
	}
	d, err := parser.Parse()
	if err != nil {
		return nil, errors.Newf(token.NoPos, "protobuf: %v", err)
	}

	tfile := token.NewFile(filename, 0, len(b))
	tfile.SetLinesForContent(b)

	p = &protoConverter{
		id:       filename,
		state:    s,
		tfile:    tfile,
		imported: map[string]bool{},
		symbols:  map[string]bool{},
		aliases:  map[string]string{},
	}

	defer func() {
		switch x := recover().(type) {
		case nil:
		case protoError:
			err = &protobufError{
				path: p.path,
				pos:  p.toCUEPos(x.pos),
				err:  x.error,
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
					failf(x.Position, "unquoting package filed: %v", err)
				}
				split := strings.Split(str, ";")
				switch {
				case strings.Contains(split[0], "."):
					p.cuePkgPath = split[0]
					switch len(split) {
					case 1:
						p.shortPkgName = path.Base(str)
					case 2:
						p.shortPkgName = split[1]
					default:
						failf(x.Position, "unexpected ';' in %q", str)
					}
					p.file.Name = ast.NewIdent(p.shortPkgName)

				case len(split) == 1:
					p.shortPkgName = split[0]
					p.file.Name = ast.NewIdent(p.shortPkgName)

				default:
					failf(x.Position, "malformed go_package clause %s", str)
				}
				// name.AddComment(comment(x.Comment, true))
				// name.AddComment(comment(x.InlineComment, false))
			}
		}
	}

	for _, e := range d.Elements {
		switch x := e.(type) {
		case *proto.Import:
			if err := p.doImport(x); err != nil {
				return nil, err
			}
		}
	}

	imports := &ast.ImportDecl{}
	p.file.Decls = append(p.file.Decls, imports)

	for _, e := range d.Elements {
		p.topElement(e)
	}

	imported := []string{}
	for k := range p.imported {
		imported = append(imported, k)
	}
	sort.Strings(imported)
	p.sorted = imported

	for _, v := range imported {
		spec := &ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(v)},
		}
		imports.Specs = append(imports.Specs, spec)
		p.file.Imports = append(p.file.Imports, spec)
	}

	if len(imports.Specs) == 0 {
		p.file.Decls = p.file.Decls[1:]
	}

	return p, nil
}

// A protoConverter converts a proto definition to CUE. Proto files map to
// CUE files one to one.
type protoConverter struct {
	state *Extractor
	tfile *token.File

	proto3 bool

	id           string
	protoPkg     string
	shortPkgName string
	cuePkgPath   string

	// w bytes.Buffer
	file   *ast.File
	inBody bool

	sorted   []string
	imported map[string]bool

	path    []string
	scope   []map[string]mapping // for symbols resolution within package.
	symbols map[string]bool      // symbols provided by package
	aliases map[string]string    // for shadowed packages
}

type mapping struct {
	ref   string
	alias string // alias for the type, if exists.
	pkg   *protoConverter
}

func (p *protoConverter) importPath() string {
	if p.cuePkgPath == "" && p.protoPkg != "" {
		dir := strings.Replace(p.protoPkg, ".", "/", -1)
		p.cuePkgPath = path.Join("googleapis.com", dir)
	}
	return p.cuePkgPath
}

func (p *protoConverter) shortName() string {
	if p.shortPkgName == "" && p.protoPkg != "" {
		split := strings.Split(p.protoPkg, ".")
		p.shortPkgName = split[len(split)-1]
		p.file.Name = ast.NewIdent(p.shortPkgName)
	}
	return p.shortPkgName
}

func (p *protoConverter) toCUEPos(pos scanner.Position) token.Pos {
	return p.tfile.Pos(pos.Offset, 0)
}

func (p *protoConverter) addRef(pos scanner.Position, from, to string) {
	top := p.scope[len(p.scope)-1]
	if _, ok := top[from]; ok {
		failf(pos, "entity %q already defined", from)
	}
	top[from] = mapping{ref: to}
}

func (p *protoConverter) addNames(elems []proto.Visitee) {
	p.scope = append(p.scope, map[string]mapping{})
	for _, e := range elems {
		var pos scanner.Position
		var name string
		switch x := e.(type) {
		case *proto.Message:
			if x.IsExtend {
				continue
			}
			name = x.Name
			pos = x.Position
		case *proto.Enum:
			name = x.Name
			pos = x.Position
		case *proto.NormalField:
			name = x.Name
			pos = x.Position
		case *proto.MapField:
			name = x.Name
			pos = x.Position
		case *proto.Oneof:
			name = x.Name
			pos = x.Position
		default:
			continue
		}
		sym := strings.Join(append(p.path, name), ".")
		p.symbols[sym] = true
		p.addRef(pos, name, strings.Join(append(p.path, name), "_"))
	}
}

func (p *protoConverter) popNames() {
	p.scope = p.scope[:len(p.scope)-1]
}

func (p *protoConverter) uniqueTop(name string) string {
	a := strings.SplitN(name, ".", 2)
	for i := len(p.scope) - 1; i > 0; i-- {
		if _, ok := p.scope[i][a[0]]; ok {
			first := a[0]
			alias, ok := p.aliases[first]
			if !ok {
				// TODO: this is likely to be okay, but find something better.
				alias = "__" + first
				p.file.Decls = append(p.file.Decls, &ast.Alias{
					Ident: ast.NewIdent(alias),
					Expr:  ast.NewIdent(first),
				})
				p.aliases[first] = alias
			}
			if len(a) > 1 {
				alias += "." + a[1]
			}
			return alias
		}
	}
	return name
}

func (p *protoConverter) toExpr(pos scanner.Position, name string) (expr ast.Expr) {
	a := strings.Split(name, ".")
	for i, s := range a {
		if i == 0 {
			expr = &ast.Ident{NamePos: p.toCUEPos(pos), Name: s}
			continue
		}
		expr = &ast.SelectorExpr{X: expr, Sel: ast.NewIdent(s)}
	}
	return expr
}

func (p *protoConverter) resolve(pos scanner.Position, name string, options []*proto.Option) string {
	if s, ok := protoToCUE(name, options); ok {
		return p.uniqueTop(s)
	}
	if strings.HasPrefix(name, ".") {
		return p.resolveTopScope(pos, name[1:], options)
	}
	for i := len(p.scope) - 1; i > 0; i-- {
		if m, ok := p.scope[i][name]; ok {
			cueName := strings.Replace(m.ref, ".", "_", -1)
			return cueName
		}
	}
	return p.resolveTopScope(pos, name, options)
}

func (p *protoConverter) resolveTopScope(pos scanner.Position, name string, options []*proto.Option) string {
	for i := 0; i < len(name); i++ {
		k := strings.IndexByte(name[i:], '.')
		i += k
		if k == -1 {
			i = len(name)
		}
		if m, ok := p.scope[0][name[:i]]; ok {
			if m.pkg != nil {
				p.imported[m.pkg.importPath()] = true
				// TODO: do something more principled.
			}
			cueName := strings.Replace(name[i:], ".", "_", -1)
			return p.uniqueTop(m.ref + cueName)
		}
	}
	failf(pos, "name %q not found", name)
	return ""
}

func (p *protoConverter) doImport(v *proto.Import) error {
	if v.Filename == "cue/cue.proto" {
		return nil
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
		err := errors.Newf(p.toCUEPos(v.Position), "could not find import %q", v.Filename)
		p.state.addErr(err)
		return err
	}

	if !p.mapBuiltinPackage(v.Position, v.Filename, filename == "") {
		return nil
	}

	imp, err := p.state.parse(filename, nil)
	if err != nil {
		fail(v.Position, err)
	}

	prefix := ""
	if imp.importPath() != p.importPath() {
		prefix = imp.shortName() + "."
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
				if imp.importPath() == p.importPath() {
					pkg = nil
				}
				p.scope[0][ref] = mapping{prefix + k, "", pkg}
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
	return nil
}

func (p *protoConverter) stringLit(pos scanner.Position, s string) *ast.BasicLit {
	return &ast.BasicLit{
		ValuePos: p.toCUEPos(pos),
		Kind:     token.STRING,
		Value:    strconv.Quote(s)}
}

func (p *protoConverter) ident(pos scanner.Position, name string) *ast.Ident {
	return &ast.Ident{NamePos: p.toCUEPos(pos), Name: labelName(name)}
}

func (p *protoConverter) ref(pos scanner.Position) *ast.Ident {
	return &ast.Ident{NamePos: p.toCUEPos(pos), Name: strings.Join(p.path, "_")}
}

func (p *protoConverter) subref(pos scanner.Position, name string) *ast.Ident {
	return &ast.Ident{
		NamePos: p.toCUEPos(pos),
		Name:    strings.Join(append(p.path, name), "_"),
	}
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

	case *proto.Service:
		// TODO: handle services.

	case *proto.Extensions, *proto.Reserved:
		// no need to handle

	default:
		failf(scanner.Position{}, "unsupported type %T", x)
	}
}

func (p *protoConverter) message(v *proto.Message) {
	if v.IsExtend {
		// TODO: we are not handling extensions as for now.
		return
	}

	defer func(saved []string) { p.path = saved }(p.path)
	p.path = append(p.path, v.Name)

	p.addNames(v.Elements)
	defer p.popNames()

	// TODO: handle IsExtend/ proto2

	s := &ast.StructLit{
		Lbrace: p.toCUEPos(v.Position),
		// TOOD: set proto file position.
		Rbrace: token.Newline.Pos(),
	}

	ref := p.ref(v.Position)
	if v.Comment == nil {
		ref.NamePos = newSection
	}
	f := &ast.Field{Label: ref, Value: s}
	addComments(f, 1, v.Comment, nil)

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
				Lbrack:   p.toCUEPos(x.Position),
				Ellipsis: token.NoSpace.Pos(),
				Type:     f.Value,
			}
		}

	case *proto.MapField:
		defer func(saved []string) { p.path = saved }(p.path)
		p.path = append(p.path, x.Name)

		f := &ast.Field{}

		// All keys are converted to strings.
		// TODO: support integer keys.
		f.Label = &ast.TemplateLabel{Ident: ast.NewIdent("_")}
		f.Value = p.toExpr(x.Position, p.resolve(x.Position, x.Type, x.Options))

		name := p.ident(x.Position, x.Name)
		f = &ast.Field{
			Label: name,
			Value: &ast.StructLit{Elts: []ast.Decl{f}},
		}
		addComments(f, i, x.Comment, x.InlineComment)

		o := optionParser{message: s, field: f}
		o.tags = fmt.Sprintf("%d,type=map<%s,%s>", x.Sequence, x.KeyType, x.Type)
		if x.Name != name.Name {
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

	case *proto.Extensions, *proto.Reserved:
		// no need to handle

	default:
		failf(scanner.Position{}, "unsupported field type %T", v)
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
		failf(x.Position, "empty enum")
	}

	name := p.subref(x.Position, x.Name)

	defer func(saved []string) { p.path = saved }(p.path)
	p.path = append(p.path, x.Name)

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

	if strings.Contains(name.Name, "google") {
		panic(name.Name)
	}
	p.file.Decls = append(p.file.Decls, enum, d)

	numEnums := 0
	for _, v := range x.Elements {
		if _, ok := v.(*proto.EnumField); ok {
			numEnums++
		}
	}

	// The line comments for an enum field need to attach after the '|', which
	// is only known at the next iteration.
	var lastComment *proto.Comment
	for i, v := range x.Elements {
		switch y := v.(type) {
		case *proto.EnumField:
			// Add enum value to map
			f := &ast.Field{
				Label: p.stringLit(y.Position, y.Name),
				Value: &ast.BasicLit{Value: strconv.Itoa(y.Integer)},
			}
			valueMap.Elts = append(valueMap.Elts, f)

			// add to enum disjunction
			value := p.stringLit(y.Position, y.Name)

			var e ast.Expr = value
			// Make the first value the default value.
			if i == 0 {
				e = value
				if numEnums > 1 {
					e = &ast.UnaryExpr{OpPos: newline, Op: token.MUL, X: value}
				}
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

// oneOf converts a Proto OneOf field to CUE. Note that Protobuf defines
// a oneOf to be at most one of the fields. Rather than making each field
// optional, we define oneOfs as all required fields, but add one more
// disjunction allowing no fields. This makes it easier to constrain the
// result to include at least one of the values.
func (p *protoConverter) oneOf(x *proto.Oneof) {
	f := &ast.Field{
		Label: p.ref(x.Position),
		// TODO: Once we have closed structs, a oneOf is represented as a
		// disjunction of empty structs and closed structs with required fields.
		// For now we just specify the required fields. This is not correct
		// but more practical.
		// Value: &ast.StructLit{}, // Remove to make at least one required.
	}
	f.AddComment(comment(x.Comment, true))

	p.file.Decls = append(p.file.Decls, f)

	for _, v := range x.Elements {
		s := &ast.StructLit{
			// TODO: make this the default in the formatter.
			Rbrace: token.Newline.Pos(),
		}
		switch x := v.(type) {
		case *proto.OneOfField:
			oneOf := p.parseField(s, 0, x.Field)
			oneOf.Optional = token.NoPos

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
	defer func(saved []string) { p.path = saved }(p.path)
	p.path = append(p.path, x.Name)

	f := &ast.Field{}
	addComments(f, i, x.Comment, x.InlineComment)

	name := p.ident(x.Position, x.Name)
	f.Label = name
	typ := p.resolve(x.Position, x.Type, x.Options)
	f.Value = p.toExpr(x.Position, typ)
	s.Elts = append(s.Elts, f)

	o := optionParser{message: s, field: f}

	// body of @protobuf tag: sequence[,type][,name=<name>][,...]
	o.tags += fmt.Sprint(x.Sequence)
	if x.Type != typ {
		o.tags += ",type=" + x.Type
	}
	if x.Name != name.Name {
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
				failf(o.Position, "invalid cue.val value: %v", err)
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
			p.tags += ","
			switch source {
			case "true":
				p.tags += quoteOption(o.Name)
			default:
				p.tags += quoteOption(o.Name + "=" + source)
			}
		}
	}
}

func quoteOption(s string) string {
	needQuote := false
	for _, r := range s {
		if !unicode.In(r, unicode.L, unicode.N) {
			needQuote = true
			break
		}
	}
	if !needQuote {
		return s
	}
	if !strings.ContainsAny(s, `"\`) {
		return strconv.Quote(s)
	}
	esc := `\#`
	for strings.Contains(s, esc) {
		esc += "#"
	}
	return esc[1:] + `"` + s + `"` + esc[1:]
}
