// Package goplugin implements the entry point for Go/CUE colocated
// plugins.
//
// This package is designed to be called from generated code, not
// user-written code.
//
// Note: EXPERIMENTAL API
package goplugin

import (
	"go/parser"
	gotoken "go/token"
	"iter"
	"path"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/value"
)

// Reference holds a reference from an @go(...) attribute
// within a given CUE instance to a value defined in a Go package.
type Reference struct {
	// GoPackage holds the Go package path being referred to (without any semver).
	// It can be "." when referring to the Go package co-located with its
	// CUE package, in which case we don't know the full Go package
	// name and have to key off CUEInstance instead.
	GoPackage string

	// CUEInstance holds the path of the CUE instance (package)
	// when GoPackage is ".", and is empty otherwise.
	CUEInstance string

	// CUEModuleVersion holds the module/version pair
	// of the CUE module that the reference has
	// been made from. The version part will be empty
	// if the reference is from the main module.
	CUEModuleVersion string

	// Name holds the name to import from the Go package.
	Name string
}

// References returns a sequence of all the references found in the given
// instance.
func References(inst *build.Instance) iter.Seq2[Reference, error] {
	return func(yield func(Reference, error) bool) {
		b := refBuilder{
			inst:       inst,
			modVersion: inst.ModuleVersion.String(),
		}
		for _, f := range inst.Files {
			extAttrs, err := runtime.ExternAttrsForFile(f)
			if err != nil {
				if !yield(Reference{}, err) {
					return
				}
				continue
			}
			if extAttrs.TopLevel["go"] == nil {
				continue
			}
			for attr := range extAttrs.Body {
				if attr.Attr.Name != "go" {
					continue
				}
				ref, err := b.referenceForAttr(&attr)
				if err != nil {
					if !yield(Reference{}, err) {
						return
					}
					continue
				}
				if !yield(ref, nil) {
					return
				}
			}
		}
	}
}

// Injection returns a [cuecontext.Injection] that provides Go plugin
// values to CUE code marked with @extern(go) and @go(...) attributes.
//
// The refMap maps references to functions that return the corresponding
// CUE values. It is typically populated by generated code.
func Injection(refMap map[Reference]func() cue.Value) cuecontext.Injection {
	return &goInjection{
		refMap: refMap,
	}
}

type goInjection struct {
	refMap map[Reference]func() cue.Value
}

func (ij *goInjection) Kind() string {
	return "go"
}

func (ij *goInjection) InjectorForInstance(inst *build.Instance, r *runtime.Runtime) (runtime.Injector, errors.Error) {
	return &goInjector{
		injection: ij,
		builder: refBuilder{
			inst:       inst,
			modVersion: inst.ModuleVersion.String(),
		},
	}, nil
}

type goInjector struct {
	builder   refBuilder
	injection *goInjection
}

func (ij *goInjector) InjectedValue(attr *runtime.ExternAttr, scope *adt.Vertex) (adt.Expr, errors.Error) {
	ref, err := ij.builder.referenceForAttr(attr)
	if err != nil {
		return nil, err
	}
	getValue, ok := ij.injection.refMap[ref]
	if !ok {
		if ref.GoPackage == "." {
			return nil, errors.Newf(attr.Attr.Pos, "no Go plugin registered for %s in Go package in CUE instance %s in module %s", ref.Name, ref.CUEInstance, ref.CUEModuleVersion)
		} else {
			return nil, errors.Newf(attr.Attr.Pos, "no Go plugin registered for %s in Go package %q in module %s", ref.Name, ref.GoPackage, ref.CUEModuleVersion)
		}
	}

	_, vertex := value.ToInternal(getValue())
	return vertex, nil
}

type refBuilder struct {
	inst       *build.Instance
	modVersion string
	topLevel   map[*internal.Attr]*topLevelInfo
}

type topLevelInfo struct {
	imports map[string]string // identifier -> full Go package path
	err     errors.Error
}

func (b *refBuilder) referenceForAttr(attr *runtime.ExternAttr) (Reference, errors.Error) {
	info := b.parseTopLevel(attr.TopLevel)
	if info.err != nil {
		return Reference{}, info.err
	}

	a := attr.Attr
	if a.Err != nil {
		return Reference{}, a.Err
	}

	arg, err := a.String(0)
	if err != nil {
		return Reference{}, errors.Newf(a.Pos, "invalid @go attribute: %v", err)
	}

	var goPackage, name string
	switch {
	case arg == "":
		// @go() — use field name, local package.
		f, ok := attr.Parent.(*ast.Field)
		if !ok {
			return Reference{}, errors.Newf(a.Pos, "@go() with no argument must be on a field")
		}
		labelName, _, lerr := ast.LabelName(f.Label)
		if lerr != nil || labelName == "" {
			return Reference{}, errors.Newf(a.Pos, "cannot determine field name for @go()")
		}
		name = labelName
		goPackage = "."

	case strings.Contains(arg, "."):
		// @go(pkg.Name) — qualified reference.
		pkgIdent, funcName, _ := strings.Cut(arg, ".")
		if funcName == "" {
			return Reference{}, errors.Newf(a.Pos, "missing function name in @go(%s)", arg)
		}
		pkgPath, ok := info.imports[pkgIdent]
		if !ok {
			return Reference{}, errors.Newf(a.Pos, "unknown import identifier %q in @go(%s)", pkgIdent, arg)
		}
		goPackage = pkgPath
		name = funcName

	default:
		// @go(Name) — unqualified, local package.
		name = arg
		goPackage = "."
	}

	ref := Reference{
		GoPackage:        goPackage,
		CUEModuleVersion: b.modVersion,
		Name:             name,
	}
	if ref.GoPackage == "." {
		if b.inst.ImportPath == "" {
			return Reference{}, errors.Newf(a.Pos, "local Go plugin reference requires a CUE module")
		}
		ref.CUEInstance = b.inst.ImportPath
	}
	return ref, nil
}

func (b *refBuilder) parseTopLevel(attr *internal.Attr) *topLevelInfo {
	if info := b.topLevel[attr]; info != nil {
		return info
	}
	if b.topLevel == nil {
		b.topLevel = make(map[*internal.Attr]*topLevelInfo)
	}
	info := doParseTopLevel(attr)
	b.topLevel[attr] = info
	return info
}

func doParseTopLevel(attr *internal.Attr) *topLevelInfo {
	if attr.Err != nil {
		return &topLevelInfo{err: attr.Err}
	}

	// Find the import field in the @extern(go, import=(...)) attribute.
	var rawImports string
	for i := range attr.Fields {
		if attr.Fields[i].Key() == "import" {
			rawImports = attr.Fields[i].RawValue()
			break
		}
	}
	if rawImports == "" {
		return &topLevelInfo{}
	}

	src := "package p\nimport " + rawImports
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return &topLevelInfo{
			err: errors.Newf(attr.Pos, "cannot parse Go import declaration: %v", err),
		}
	}

	imports := make(map[string]string)
	for _, spec := range f.Imports {
		pkgPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return &topLevelInfo{
				err: errors.Newf(attr.Pos, "invalid import path %s: %v", spec.Path.Value, err),
			}
		}
		var ident string
		if spec.Name != nil {
			ident = spec.Name.Name
			if ident == "." || ident == "_" {
				return &topLevelInfo{
					err: errors.Newf(attr.Pos, "unsupported import name %q for %q", ident, pkgPath),
				}
			}
		} else {
			ident = defaultImportName(pkgPath)
		}
		if _, exists := imports[ident]; exists {
			return &topLevelInfo{
				err: errors.Newf(attr.Pos, "duplicate import identifier %q", ident),
			}
		}
		imports[ident] = pkgPath
	}
	return &topLevelInfo{imports: imports}
}

// defaultImportName returns the default identifier for a Go import path.
// For v2+ module paths like "github.com/foo/bar/v2", it returns "bar"
// rather than "v2", matching Go's actual package naming convention.
func defaultImportName(pkgPath string) string {
	base := path.Base(pkgPath)
	if isSemverMajor(base) {
		return path.Base(path.Dir(pkgPath))
	}
	return base
}

// isSemverMajor reports whether s looks like a Go module major version
// suffix (v2, v3, ..., but not v0 or v1).
func isSemverMajor(s string) bool {
	if len(s) < 2 || s[0] != 'v' || s[1] < '2' || s[1] > '9' {
		return false
	}
	for _, c := range s[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
