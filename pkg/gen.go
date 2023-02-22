// Copyright 2018 The CUE Authors
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

//go:build ignore

// gen.go generates the pkg.go files inside the packages under the pkg directory.
//
// It takes the list of packages from the packages.txt.
//
// Be sure to also update an entry in pkg/pkg.go, if so desired.
package main

// TODO generate ../register.go too.

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/ast"
	gobuild "go/build"
	"go/constant"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	cueformat "cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
)

const genFile = "pkg.go"

//go:embed packages.txt
var packagesStr string

var packages = strings.Fields(packagesStr)

type headerParams struct {
	GoPkg  string
	CUEPkg string

	PackageDoc  string
	PackageDefs string
}

var header = template.Must(template.New("").Parse(
	`// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

{{if .PackageDoc}}
{{.PackageDoc -}}
//     {{.PackageDefs}}
{{end -}}
package {{.GoPkg}}

{{if .CUEPkg -}}
import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register({{printf "%q" .CUEPkg}}, p)
}

var _ = adt.TopKind // in case the adt package isn't used
{{end}}
`))

func main() {
	flag.Parse()
	log.SetFlags(log.Lshortfile)
	log.SetOutput(os.Stdout)

	for _, pkg := range packages {
		if pkg == "path" {
			// TODO remove this special case. Currently the path
			// pkg.go file cannot be generated automatically but that
			// will be possible when we can attach arbitrary signatures
			// to builtin functions.
			continue
		}
		if err := generate(pkg); err != nil {
			log.Fatalf("%s: %v", pkg, err)
		}
	}
}

type generator struct {
	dir        string
	w          *bytes.Buffer
	cuePkgPath string
	fset       *token.FileSet
	first      bool
}

func generate(cuePkgPath string) error {
	goPkgPath := path.Join("cuelang.org/go/pkg", cuePkgPath)
	pkg, err := gobuild.Import(goPkgPath, "", 0)
	if err != nil {
		return err
	}

	g := generator{
		dir:        pkg.Dir,
		cuePkgPath: cuePkgPath,
		w:          &bytes.Buffer{},
		fset:       token.NewFileSet(),
	}

	params := headerParams{
		GoPkg:  pkg.Name,
		CUEPkg: cuePkgPath,
	}
	// As a special case, the "tool" package cannot be imported from CUE.
	skipRegister := params.CUEPkg == "tool"
	if skipRegister {
		params.CUEPkg = ""
	}

	if doc, err := os.ReadFile(filepath.Join(pkg.Dir, "doc.txt")); err == nil {
		defs, err := os.ReadFile(filepath.Join(pkg.Dir, pkg.Name+".cue"))
		if err != nil {
			return err
		}
		i := bytes.Index(defs, []byte("package "+pkg.Name))
		defs = defs[i+len("package "+pkg.Name)+1:]
		defs = bytes.TrimRight(defs, "\n")
		defs = bytes.ReplaceAll(defs, []byte("\n"), []byte("\n//\t"))
		params.PackageDoc = string(doc)
		params.PackageDefs = string(defs)
	}

	if err := header.Execute(g.w, params); err != nil {
		return err
	}

	if !skipRegister {
		fmt.Fprintf(g.w, "var p = &pkg.Package{\nNative: []*pkg.Builtin{")
		g.first = true
		for _, filename := range pkg.GoFiles {
			if filename == genFile {
				continue
			}
			g.processGo(filepath.Join(pkg.Dir, filename))
		}
		fmt.Fprintf(g.w, "},\n")
		if err := g.processCUE(); err != nil {
			return err
		}
		fmt.Fprintf(g.w, "}\n")
	}

	b, err := format.Source(g.w.Bytes())
	if err != nil {
		b = g.w.Bytes() // write the unformatted source
	}

	filename := filepath.Join(pkg.Dir, genFile)

	if err := ioutil.WriteFile(filename, b, 0666); err != nil {
		return err
	}
	return nil
}

func (g *generator) sep() {
	if g.first {
		g.first = false
		return
	}
	fmt.Fprint(g.w, ", ")
}

// processCUE mixes in CUE definitions defined in the package directory.
func (g *generator) processCUE() error {
	// Note: we avoid using the cue/load and the cuecontext packages
	// because they depend on the standard library which is what this
	// command is generating - cyclic dependencies are undesirable in general.
	ctx := newContext()
	val, err := loadCUEPackage(ctx, g.dir, g.cuePkgPath)
	if err != nil {
		if errors.Is(err, errNoCUEFiles) {
			return nil
		}
		errors.Print(os.Stderr, err, nil)
		return fmt.Errorf("error processing %s: %v", g.cuePkgPath, err)
	}

	v := val.Syntax(cue.Raw())
	// fmt.Printf("%T\n", v)
	// fmt.Println(astinternal.DebugStr(v))
	n := internal.ToExpr(v)
	b, err := cueformat.Node(n)
	if err != nil {
		return err
	}
	b = bytes.ReplaceAll(b, []byte("\n\n"), []byte("\n"))
	// body = strings.ReplaceAll(body, "\t", "")
	// TODO: escape backtick
	fmt.Fprintf(g.w, "CUE: `%s`,\n", string(b))
	return nil
}

func (g *generator) processGo(filename string) error {
	f, err := parser.ParseFile(g.fset, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.GenDecl:
			switch x.Tok {
			case token.CONST:
				for _, spec := range x.Specs {
					spec := spec.(*ast.ValueSpec)
					if ast.IsExported(spec.Names[0].Name) {
						g.genConst(spec)
					}
				}
			case token.VAR:
				continue
			case token.TYPE:
				// TODO: support type declarations.
				continue
			case token.IMPORT:
				continue
			default:
				panic(fmt.Errorf("gen %s: unexpected spec of type %s", filename, x.Tok))
			}
		case *ast.FuncDecl:
			g.genFunc(x)
		}
	}
	return nil
}

func (g *generator) genConst(spec *ast.ValueSpec) {
	name := spec.Names[0].Name
	value := ""
	switch v := g.toValue(spec.Values[0]); v.Kind() {
	case constant.Bool, constant.Int, constant.String:
		// TODO: convert octal numbers
		value = v.ExactString()
	case constant.Float:
		var rat big.Rat
		rat.SetString(v.ExactString())
		var float big.Float
		float.SetRat(&rat)
		value = float.Text('g', -1)
	default:
		fmt.Printf("Dropped entry %s.%s (%T: %v)\n", g.cuePkgPath, name, v.Kind(), v.ExactString())
		return
	}
	g.sep()
	fmt.Fprintf(g.w, "{\nName: %q,\n Const: %q,\n}", name, value)
}

func (g *generator) toValue(x ast.Expr) constant.Value {
	switch x := x.(type) {
	case *ast.BasicLit:
		return constant.MakeFromLiteral(x.Value, x.Kind, 0)
	case *ast.BinaryExpr:
		return constant.BinaryOp(g.toValue(x.X), x.Op, g.toValue(x.Y))
	case *ast.UnaryExpr:
		return constant.UnaryOp(x.Op, g.toValue(x.X), 0)
	default:
		panic(fmt.Errorf("%s: unsupported expression type %T: %#v", g.cuePkgPath, x, x))
	}
}

func (g *generator) genFunc(x *ast.FuncDecl) {
	if x.Body == nil || !ast.IsExported(x.Name.Name) {
		return
	}
	types := []string{}
	if x.Type.Results != nil {
		for _, f := range x.Type.Results.List {
			if len(f.Names) > 0 {
				for range f.Names {
					types = append(types, g.goKind(f.Type))
				}
			} else {
				types = append(types, g.goKind(f.Type))
			}
		}
	}
	if x.Recv != nil {
		return
	}
	if n := len(types); n != 1 && (n != 2 || types[1] != "error") {
		fmt.Printf("Dropped func %s.%s: must have one return value or a value and an error %v\n", g.cuePkgPath, x.Name.Name, types)
		return
	}

	g.sep()
	fmt.Fprintf(g.w, "{\n")
	defer fmt.Fprintf(g.w, "}")

	fmt.Fprintf(g.w, "Name: %q,\n", x.Name.Name)

	args := []string{}
	vals := []string{}
	kind := []string{}
	for _, f := range x.Type.Params.List {
		for _, name := range f.Names {
			typ := strings.Title(g.goKind(f.Type))
			argKind := g.goToCUE(f.Type)
			vals = append(vals, fmt.Sprintf("c.%s(%d)", typ, len(args)))
			args = append(args, name.Name)
			kind = append(kind, argKind)
		}
	}

	fmt.Fprintf(g.w, "Params: []pkg.Param{\n")
	for _, k := range kind {
		fmt.Fprintf(g.w, "{Kind: %s},\n", k)
	}
	fmt.Fprintf(g.w, "\n},\n")

	expr := x.Type.Results.List[0].Type
	fmt.Fprintf(g.w, "Result: %s,\n", g.goToCUE(expr))

	argList := strings.Join(args, ", ")
	valList := strings.Join(vals, ", ")
	init := ""
	if len(args) > 0 {
		init = fmt.Sprintf("%s := %s", argList, valList)
	}

	fmt.Fprintf(g.w, "Func: func(c *pkg.CallCtxt) {")
	defer fmt.Fprintln(g.w, "},")
	fmt.Fprintln(g.w)
	if init != "" {
		fmt.Fprintln(g.w, init)
	}
	fmt.Fprintln(g.w, "if c.Do() {")
	defer fmt.Fprintln(g.w, "}")
	if len(types) == 1 {
		fmt.Fprintf(g.w, "c.Ret = %s(%s)", x.Name.Name, argList)
	} else {
		fmt.Fprintf(g.w, "c.Ret, c.Err = %s(%s)", x.Name.Name, argList)
	}
}

func (g *generator) goKind(expr ast.Expr) string {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}
	w := &bytes.Buffer{}
	printer.Fprint(w, g.fset, expr)
	switch str := w.String(); str {
	case "big.Int":
		return "bigInt"
	case "big.Float":
		return "bigFloat"
	case "big.Rat":
		return "bigRat"
	case "adt.Bottom":
		return "error"
	case "internal.Decimal":
		return "decimal"
	case "pkg.List":
		return "cueList"
	case "pkg.Struct":
		return "struct"
	case "[]*internal.Decimal":
		return "decimalList"
	case "cue.Value":
		return "value"
	case "cue.List":
		return "list"
	case "[]string":
		return "stringList"
	case "[]byte":
		return "bytes"
	case "[]cue.Value":
		return "list"
	case "io.Reader":
		return "reader"
	case "time.Time":
		return "string"
	default:
		return str
	}
}

func (g *generator) goToCUE(expr ast.Expr) (cueKind string) {
	// TODO: detect list and structs types for return values.
	switch k := g.goKind(expr); k {
	case "error":
		cueKind += "adt.BottomKind"
	case "bool":
		cueKind += "adt.BoolKind"
	case "bytes", "reader":
		cueKind += "adt.BytesKind|adt.StringKind"
	case "string":
		cueKind += "adt.StringKind"
	case "int", "int8", "int16", "int32", "rune", "int64",
		"uint", "byte", "uint8", "uint16", "uint32", "uint64",
		"bigInt":
		cueKind += "adt.IntKind"
	case "float64", "bigRat", "bigFloat", "decimal":
		cueKind += "adt.NumKind"
	case "list", "decimalList", "stringList", "cueList":
		cueKind += "adt.ListKind"
	case "struct":
		cueKind += "adt.StructKind"
	case "value":
		// Must use callCtxt.value method for these types and resolve manually.
		cueKind += "adt.TopKind" // TODO: can be more precise
	default:
		switch {
		case strings.HasPrefix(k, "[]"):
			cueKind += "adt.ListKind"
		case strings.HasPrefix(k, "map["):
			cueKind += "adt.StructKind"
		default:
			// log.Println("Unknown type:", k)
			// Must use callCtxt.value method for these types and resolve manually.
			cueKind += "adt.TopKind" // TODO: can be more precise
		}
	}
	return cueKind
}

var errNoCUEFiles = errors.New("no CUE files in directory")

// loadCUEPackage loads a CUE package as a value. We avoid using cue/load because
// that depends on the standard library and as this generator is generating the standard
// library, we don't want that cyclic dependency.
// It only has to deal with the fairly limited subset of CUE packages that are
// present inside pkg/....
func loadCUEPackage(ctx *cue.Context, dir string, pkgPath string) (cue.Value, error) {
	inst := &build.Instance{
		PkgName:     path.Base(pkgPath),
		Dir:         dir,
		DisplayPath: pkgPath,
		ImportPath:  pkgPath,
	}
	cuefiles, err := filepath.Glob(filepath.Join(dir, "*.cue"))
	if err != nil {
		return cue.Value{}, err
	}
	if len(cuefiles) == 0 {
		return cue.Value{}, errNoCUEFiles
	}
	for _, file := range cuefiles {
		if err := inst.AddFile(file, nil); err != nil {
			return cue.Value{}, err
		}
	}
	if err := inst.Complete(); err != nil {
		return cue.Value{}, err
	}
	vals, err := ctx.BuildInstances([]*build.Instance{inst})
	if err != nil {
		return cue.Value{}, err
	}
	return vals[0], nil
}

// Avoid using cuecontext.New because that package imports
// the entire stdlib which we are generating.
func newContext() *cue.Context {
	r := runtime.New()
	return (*cue.Context)(r)
}
