package cmd

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"

	cueast "cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/load"
	cuetoken "cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
)

func newPluginGenerateGoCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate-go",
		Short: "generate Go and CUE files from @extern(go) declarations",
		Long: `Generate-go reads @extern(go) and @go(...) declarations in the CUE package
in the current directory and generates two files per source:

  <stem>_signatures_gen.cue  CUE shadow fields with function signature doc comments
  <stem>_imports_gen.go      Go file with blank imports for referenced packages

The <stem> is derived from the CUE source file containing the @extern(go)
declaration (e.g. sprig.cue produces sprig_signatures_gen.cue and
sprig_imports_gen.go).
`,
		RunE: mkRunE(c, runPluginGenerateGo),
		Args: cobra.ExactArgs(0),
	}
	return cmd
}

// genRef holds info about one @go reference in a CUE file.
type genRef struct {
	fieldName  string
	required   bool
	goPackage  string // resolved Go package path, or "." for local
	goFuncName string
	hasDoc     bool // whether the CUE field already has a doc comment
}

// genFile holds all references from a single CUE file that has @extern(go).
type genFile struct {
	filename string
	imports  map[string]string // go import ident -> go package path
	refs     []genRef
}

func runPluginGenerateGo(cmd *Command, args []string) error {
	cfg := &load.Config{}
	insts := load.Instances([]string{"."}, cfg)
	if len(insts) == 0 || insts[0].PkgName == "" {
		return fmt.Errorf("no CUE package found in current directory")
	}
	inst := insts[0]
	if inst.Err != nil {
		return inst.Err
	}

	genFiles, err := extractGenFiles(inst)
	if err != nil {
		return err
	}
	if len(genFiles) == 0 {
		return fmt.Errorf("no @extern(go) declarations found in package %s", inst.PkgName)
	}

	// Collect all unique Go packages to load.
	allPkgs := map[string]bool{}
	hasLocal := false
	for _, gf := range genFiles {
		for _, ref := range gf.refs {
			if ref.goPackage == "." {
				hasLocal = true
			} else {
				allPkgs[ref.goPackage] = true
			}
		}
	}

	// Resolve local Go package path and find go.mod directory.
	var localGoPkg string
	var goModDir string
	if hasLocal || len(allPkgs) > 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		modRoot := inst.Root
		if modRoot == "" {
			modRoot = cwd
		}
		goMod, err := findGoMod(cwd, map[string]goModule{}, modRoot)
		if err != nil {
			return fmt.Errorf("finding go.mod: %v", err)
		}
		goModDir = goMod.dir
		if hasLocal {
			rel, err := filepath.Rel(goMod.dir, cwd)
			if err != nil {
				return err
			}
			localGoPkg = path.Join(goMod.path(), filepath.ToSlash(rel))
			allPkgs[localGoPkg] = true
		}
	}

	// Load Go packages with type info and syntax (for doc comments).
	pkgPaths := slices.Sorted(maps.Keys(allPkgs))
	loadCfg := &packages.Config{
		Dir:  goModDir,
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax,
	}
	pkgs, err := packages.Load(loadCfg, pkgPaths...)
	if err != nil {
		return fmt.Errorf("loading Go packages: %v", err)
	}
	pkgMap := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("errors loading package %s: %v", pkg.PkgPath, pkg.Errors[0])
		}
		pkgMap[pkg.PkgPath] = pkg
	}

	for _, gf := range genFiles {
		if err := generateForFile(inst, gf, localGoPkg, pkgMap); err != nil {
			return err
		}
	}
	return nil
}

type sigEntry struct {
	goDoc      string // Go doc comment (included if CUE field has none)
	sigComment string // CUE function signature comment
	fieldName  string
	required   bool
}

func generateForFile(inst *build.Instance, gf genFile, localGoPkg string, pkgMap map[string]*packages.Package) error {
	stem := strings.TrimSuffix(filepath.Base(gf.filename), ".cue")
	dir := filepath.Dir(gf.filename)

	externalPkgs := map[string]bool{}
	var entries []sigEntry

	for _, ref := range gf.refs {
		goPkgPath := ref.goPackage
		if goPkgPath == "." {
			goPkgPath = localGoPkg
		} else {
			externalPkgs[goPkgPath] = true
		}

		pkg := pkgMap[goPkgPath]
		if pkg == nil {
			return fmt.Errorf("package %s not loaded", goPkgPath)
		}

		obj := pkg.Types.Scope().Lookup(ref.goFuncName)
		if obj == nil {
			return fmt.Errorf("function %s not found in package %s", ref.goFuncName, goPkgPath)
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return fmt.Errorf("%s.%s is not a function", goPkgPath, ref.goFuncName)
		}

		sig := fn.Type().(*types.Signature)
		sigComment := formatCUESignature(sig)

		var goDoc string
		if !ref.hasDoc {
			goDoc = findGoFuncDoc(pkg, ref.goFuncName)
		}

		entries = append(entries, sigEntry{
			goDoc:      goDoc,
			sigComment: sigComment,
			fieldName:  ref.fieldName,
			required:   ref.required,
		})
	}

	// Generate <stem>_signatures_gen.cue.
	cueData := generateSignaturesCUE(inst.PkgName, entries)
	cuePath := filepath.Join(dir, stem+"_signatures_gen.cue")
	if err := os.WriteFile(cuePath, cueData, 0o666); err != nil {
		return err
	}

	// Generate <stem>_imports_gen.go.
	if len(externalPkgs) > 0 {
		goData, err := generateImportsGo(slices.Sorted(maps.Keys(externalPkgs)))
		if err != nil {
			return err
		}
		goPath := filepath.Join(dir, stem+"_imports_gen.go")
		if err := os.WriteFile(goPath, goData, 0o666); err != nil {
			return err
		}
	}
	return nil
}

func generateSignaturesCUE(pkgName string, entries []sigEntry) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by cue plugin generate-go; DO NOT EDIT.\n\n")
	fmt.Fprintf(&buf, "package %s\n", pkgName)

	for _, e := range entries {
		buf.WriteString("\n")
		if e.goDoc != "" {
			for _, line := range strings.Split(strings.TrimRight(e.goDoc, "\n"), "\n") {
				if line == "" {
					buf.WriteString("//\n")
				} else {
					fmt.Fprintf(&buf, "// %s\n", line)
				}
			}
			buf.WriteString("//\n")
		}
		fmt.Fprintf(&buf, "// %s\n", e.sigComment)
		constraint := ""
		if e.required {
			constraint = "!"
		}
		fmt.Fprintf(&buf, "%s%s: _\n", e.fieldName, constraint)
	}
	return buf.Bytes()
}

func generateImportsGo(pkgPaths []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("//go:build cueplugin\n\npackage cueplugin_imports\n\nimport (\n")
	for _, pkg := range pkgPaths {
		fmt.Fprintf(&buf, "\t_ %q\n", pkg)
	}
	buf.WriteString(")\n")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated Go file: %v", err)
	}
	return formatted, nil
}

// extractGenFiles iterates through all CUE files in the instance and
// extracts @extern(go) files with their @go references.
func extractGenFiles(inst *build.Instance) ([]genFile, error) {
	var result []genFile
	for _, f := range inst.Files {
		extAttrs, err := runtime.ExternAttrsForFile(f)
		if err != nil {
			return nil, err
		}
		goAttr := extAttrs.TopLevel["go"]
		if goAttr == nil {
			continue
		}

		gf := genFile{
			filename: f.Filename,
		}
		var parseErr error
		gf.imports, parseErr = parseGenGoImports(goAttr)
		if parseErr != nil {
			return nil, parseErr
		}

		for attr := range extAttrs.Body {
			if attr.Attr.Name != "go" {
				continue
			}
			ref, err := extractGenRef(&attr, gf.imports)
			if err != nil {
				return nil, err
			}
			gf.refs = append(gf.refs, ref)
		}
		if len(gf.refs) > 0 {
			result = append(result, gf)
		}
	}
	return result, nil
}

// parseGenGoImports parses the import=(...) field from an @extern(go) attribute.
func parseGenGoImports(goAttr *internal.Attr) (map[string]string, error) {
	var rawImports string
	for i := range goAttr.Fields {
		if goAttr.Fields[i].Key() == "import" {
			rawImports = goAttr.Fields[i].RawValue()
			break
		}
	}
	if rawImports == "" {
		return nil, nil
	}

	src := "package p\nimport " + rawImports
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, "", src, goparser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("cannot parse Go import declaration in @extern(go): %v", err)
	}

	imports := make(map[string]string)
	for _, spec := range f.Imports {
		pkgPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid import path %s: %v", spec.Path.Value, err)
		}
		var ident string
		if spec.Name != nil {
			ident = spec.Name.Name
		} else {
			ident = genDefaultGoImportName(pkgPath)
		}
		imports[ident] = pkgPath
	}
	return imports, nil
}

func extractGenRef(attr *runtime.ExternAttr, imports map[string]string) (genRef, error) {
	a := attr.Attr
	if a.Err != nil {
		return genRef{}, a.Err
	}

	arg, err := a.String(0)
	if err != nil {
		return genRef{}, fmt.Errorf("invalid @go attribute: %v", err)
	}

	f, ok := attr.Parent.(*cueast.Field)
	if !ok {
		return genRef{}, fmt.Errorf("@go attribute must be on a field")
	}

	fieldName, _, lerr := cueast.LabelName(f.Label)
	if lerr != nil || fieldName == "" {
		return genRef{}, fmt.Errorf("cannot determine field name for @go attribute")
	}

	var goPackage, goFuncName string
	switch {
	case arg == "":
		goFuncName = fieldName
		goPackage = "."
	case strings.Contains(arg, "."):
		pkgIdent, funcName, _ := strings.Cut(arg, ".")
		if funcName == "" {
			return genRef{}, fmt.Errorf("missing function name in @go(%s)", arg)
		}
		pkgPath, ok := imports[pkgIdent]
		if !ok {
			return genRef{}, fmt.Errorf("unknown import identifier %q in @go(%s)", pkgIdent, arg)
		}
		goPackage = pkgPath
		goFuncName = funcName
	default:
		goFuncName = arg
		goPackage = "."
	}

	hasDoc := false
	for _, cg := range cueast.Comments(f) {
		if cg.Doc {
			hasDoc = true
			break
		}
	}

	return genRef{
		fieldName:  fieldName,
		required:   f.Constraint == cuetoken.NOT,
		goPackage:  goPackage,
		goFuncName: goFuncName,
		hasDoc:     hasDoc,
	}, nil
}

// formatCUESignature produces a CUE-style doc comment for a Go function signature,
// e.g. "func(string, int) -> string".
func formatCUESignature(sig *types.Signature) string {
	params := sig.Params()
	results := sig.Results()

	var paramTypes []string
	for i := range params.Len() {
		paramTypes = append(paramTypes, goTypeToCUE(params.At(i).Type()))
	}

	var returnType string
	switch results.Len() {
	case 0:
		returnType = "_"
	case 1:
		returnType = goTypeToCUE(results.At(0).Type())
	case 2:
		if results.At(1).Type().String() == "error" {
			returnType = goTypeToCUE(results.At(0).Type())
		} else {
			returnType = "_"
		}
	default:
		returnType = "_"
	}

	return fmt.Sprintf("func(%s) -> %s", strings.Join(paramTypes, ", "), returnType)
}

func goTypeToCUE(t types.Type) string {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return "bool"
		case types.String:
			return "string"
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
			types.Uintptr:
			return "int"
		case types.Float32, types.Float64:
			return "float"
		}
	case *types.Slice:
		if basic, ok := t.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
			return "bytes"
		}
		return "[..." + goTypeToCUE(t.Elem()) + "]"
	case *types.Map:
		return "{[string]: " + goTypeToCUE(t.Elem()) + "}"
	case *types.Pointer:
		return "null | " + goTypeToCUE(t.Elem())
	}
	return "_"
}

// findGoFuncDoc returns the doc comment text for a function in a loaded Go package.
func findGoFuncDoc(pkg *packages.Package, funcName string) string {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			fn, ok := decl.(*goast.FuncDecl)
			if !ok || fn.Name.Name != funcName || fn.Doc == nil {
				continue
			}
			return fn.Doc.Text()
		}
	}
	return ""
}

// genDefaultGoImportName returns the default identifier for a Go import path.
func genDefaultGoImportName(pkgPath string) string {
	base := path.Base(pkgPath)
	if len(base) >= 2 && base[0] == 'v' && base[1] >= '2' && base[1] <= '9' {
		allDigits := true
		for _, c := range base[2:] {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return path.Base(path.Dir(pkgPath))
		}
	}
	return base
}
