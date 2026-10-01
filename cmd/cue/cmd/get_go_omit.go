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

package cmd

import (
	"cmp"
	"errors"
	"fmt"
	"go/types"
	"iter"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// encodingMethods lists the methods which get go considers
// when deciding whether a type encodes itself.
var encodingMethods = func() []string {
	var names []string
	for _, iface := range slices.Concat(toTop, toString) {
		names = append(names, iface.Method(0).Name())
	}
	for _, m := range jsontextMethods {
		names = append(names, m.name)
	}
	return names
}()

// omitSelector is a selector given to the --omit flag of get go.
type omitSelector struct {
	text string

	// pkgs holds the import paths of the packages declaring
	// what the selector matched.
	pkgs map[string]bool
}

// omitReading is a way to read a selector given to the --omit flag of get go,
// written as pkg.Name or pkg.Name.Method, where each element is a glob
// as understood by [path.Match].
type omitReading struct {
	// pkg is the glob for the package's import path if byPath is set,
	// or for its name otherwise.
	pkg    string
	byPath bool

	name string

	// methods holds the encoding methods matched by the method glob,
	// and is empty when the selector omits declarations.
	methods []string
}

// parseOmitSelector returns the valid ways in which text can be read,
// and why the first invalid one was rejected, if any.
// An import path may contain dots after its last slash, such as gopkg.in/yaml.v3,
// so a selector qualified by one may be read in more than one way.
func parseOmitSelector(text string) ([]omitReading, error) {
	var readings []omitReading
	var firstErr error
	add := func(r omitReading, rest string) {
		elems := strings.Split(rest, ".")
		if len(elems) > 2 {
			return
		}
		if err := r.init(elems); err != nil {
			firstErr = cmp.Or(firstErr, err)
			return
		}
		readings = append(readings, r)
	}
	if slash := strings.LastIndex(text, "/"); slash >= 0 {
		// Try each dot after the last slash as the end of the import path.
		for i := slash + 1; i < len(text); i++ {
			if text[i] == '.' {
				add(omitReading{pkg: text[:i], byPath: true}, text[i+1:])
			}
		}
	} else {
		pkgName, rest, ok := strings.Cut(text, ".")
		if !ok {
			return nil, fmt.Errorf("must be qualified by a package, such as %q", "*."+text)
		}
		add(omitReading{pkg: pkgName}, rest)
	}
	if len(readings) == 0 {
		return nil, cmp.Or(firstErr, errors.New("must be pkg.Name or pkg.Name.Method"))
	}
	return readings, firstErr
}

// init sets the name and methods of r from elems, being Name or Name and Method.
func (r *omitReading) init(elems []string) error {
	what, valid := "a Go identifier", isIdentRune
	if r.byPath {
		what, valid = "an import path", isImportPathRune
	}
	if err := checkGlob(r.pkg, what, valid); err != nil {
		return err
	}
	for _, glob := range elems {
		if err := checkGlob(glob, "a Go identifier", isIdentRune); err != nil {
			return err
		}
	}
	r.name = elems[0]
	if len(elems) == 2 {
		for _, name := range encodingMethods {
			if ok, _ := path.Match(elems[1], name); ok {
				r.methods = append(r.methods, name)
			}
		}
		if len(r.methods) == 0 {
			return fmt.Errorf("method %q must match one of %s", elems[1], strings.Join(encodingMethods, ", "))
		}
	}
	return nil
}

// checkGlob checks that glob is a valid pattern for [path.Match] which can
// match what, meaning that valid accepts each literal character in glob,
// given whether the character starts the matched string.
func checkGlob(glob, what string, valid func(r rune, start bool) bool) error {
	if glob == "" {
		return errors.New("must not have empty elements")
	}
	if _, err := path.Match(glob, ""); err != nil {
		return err
	}
	for i := 0; i < len(glob); {
		start := i == 0
		r, size := utf8.DecodeRuneInString(glob[i:])
		i += size
		switch r {
		case '*', '?':
			continue
		case '[':
			// Skip the character class, which path.Match has validated.
			for glob[i] != ']' {
				if glob[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		case '\\':
			r, size = utf8.DecodeRuneInString(glob[i:])
			i += size
		}
		if !valid(r, start) {
			return fmt.Errorf("%q can never match %s", glob, what)
		}
	}
	return nil
}

func isIdentRune(r rune, start bool) bool {
	return r == '_' || unicode.IsLetter(r) || (!start && unicode.IsDigit(r))
}

// isImportPathRune mirrors the characters which
// [golang.org/x/mod/module.CheckImportPath] allows.
func isImportPathRune(r rune, start bool) bool {
	return r < utf8.RuneSelf && (strings.ContainsRune("-._~+/", r) ||
		'0' <= r && r <= '9' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z')
}

// matches reports whether r matches obj, ignoring any methods.
func (r *omitReading) matches(obj types.Object) bool {
	pkg := obj.Pkg().Name()
	if r.byPath {
		pkg = obj.Pkg().Path()
	}
	okPkg, _ := path.Match(r.pkg, pkg)
	okName, _ := path.Match(r.name, obj.Name())
	return okPkg && okName
}

// initOmissions parses the --omit selectors given as values, each of them
// comma-separated, and records the declarations and methods they match
// as omitted. A selector which matches nothing is an error,
// as it is likely a mistake.
func (e *extractor) initOmissions(values []string) error {
	for _, value := range values {
		for text := range strings.SplitSeq(value, ",") {
			if text == "" {
				continue
			}
			readings, err := parseOmitSelector(text)
			pkgs, hint := e.omitMatching(readings)
			switch {
			case len(pkgs) > 0:
				e.omits = append(e.omits, omitSelector{text, pkgs})
			case err != nil:
				// When no valid reading matches, the invalid one is likely
				// what was meant, such as with a misspelled method.
				return fmt.Errorf("invalid --%s selector %q: %v", flagOmit, text, err)
			case hint != "":
				return fmt.Errorf("--%s selector %q matches nothing; %s", flagOmit, text, hint)
			default:
				return fmt.Errorf("--%s selector %q matches nothing", flagOmit, text)
			}
		}
	}
	return nil
}

// omitMatching records the declarations and methods matched by any of readings
// as omitted, returning the import paths of the packages declaring them,
// and a hint for when a reading would have matched the methods of an alias.
func (e *extractor) omitMatching(readings []omitReading) (pkgs map[string]bool, hint string) {
	pkgs = make(map[string]bool)
	for obj := range e.genDecls() {
		for _, r := range readings {
			if !r.matches(obj) {
				continue
			}
			tn, _ := obj.(*types.TypeName)
			switch {
			case len(r.methods) == 0:
				e.omitted[obj] = true
			case tn == nil:
				continue
			case tn.IsAlias():
				// The type which an alias denotes may be declared by another
				// package, whose generated file would then depend on
				// a selector which its recorded command does not have.
				hint = cmp.Or(hint, e.aliasHint(tn, r.methods))
				continue
			case !e.omitMethods(tn.Type(), r.methods):
				continue
			}
			pkgs[obj.Pkg().Path()] = true
		}
	}
	return pkgs, hint
}

// aliasHint suggests selecting the methods of the type which alias denotes,
// if it is a type whose methods could be omitted instead.
func (e *extractor) aliasHint(alias *types.TypeName, methods []string) string {
	named, ok := types.Unalias(alias.Type()).(*types.Named)
	if !ok {
		return ""
	}
	obj := named.Obj()
	if !e.mayGenerate(obj.Pkg().Path()) || !slices.ContainsFunc(methods, func(name string) bool {
		return hasEncodingMethod(named, name)
	}) {
		return ""
	}
	return fmt.Sprintf("%s is an alias, so select the methods of %s.%s instead",
		alias.Name(), obj.Pkg().Name(), obj.Name())
}

// omitsFor returns the --omit selectors which match in p or its dependencies,
// as the others would match nothing when generating p alone.
func (e *extractor) omitsFor(p *packages.Package) []string {
	if len(e.omits) == 0 {
		return nil
	}
	deps := make(map[string]bool)
	packages.Visit([]*packages.Package{p}, func(p *packages.Package) bool {
		deps[p.PkgPath] = true
		return true
	}, nil)
	var texts []string
	for _, sel := range e.omits {
		for pkgPath := range sel.pkgs {
			if deps[pkgPath] {
				texts = append(texts, sel.text)
				break
			}
		}
	}
	return texts
}

// genDecls yields the types and constants declared by the packages
// which get go may generate, as reported by [extractor.mayGenerate].
func (e *extractor) genDecls() iter.Seq[types.Object] {
	return func(yield func(types.Object) bool) {
		for _, p := range e.allPkgs {
			if !e.mayGenerate(p.PkgPath) {
				continue
			}
			scope := p.Types.Scope()
			for _, name := range scope.Names() {
				switch obj := scope.Lookup(name); obj.(type) {
				case *types.TypeName, *types.Const:
					if !yield(obj) {
						return
					}
				}
			}
		}
	}
}

// mayGenerate reports whether get go may generate the package at path:
// those given as arguments, held by [extractor.done] before any
// generation starts, and their dependencies outside the standard library.
func (e *extractor) mayGenerate(path string) bool {
	return e.done[path] || !isStdPkg(path)
}

// omittedMethod is an encoding method which is omitted from a type.
type omittedMethod struct {
	typ  types.Type
	name string
}

// omitMethods records which of the named encoding methods typ has as omitted,
// reporting whether there were any.
func (e *extractor) omitMethods(typ types.Type, names []string) bool {
	// A struct type has the methods promoted from its embedded fields,
	// so the omission also applies to the struct underlying typ,
	// which is how its fields are translated.
	st, _ := typ.Underlying().(*types.Struct)
	found := false
	for _, name := range names {
		if !hasEncodingMethod(typ, name) {
			continue
		}
		found = true
		e.omittedMethods[omittedMethod{typ, name}] = true
		if st != nil {
			e.omittedMethods[omittedMethod{st, name}] = true
		}
	}
	return found
}

// hasEncodingMethod reports whether typ or *typ has the encoding method name,
// with the signature that get go looks for, regardless of any omissions.
func hasEncodingMethod(typ types.Type, name string) bool {
	for _, iface := range slices.Concat(toTop, toString) {
		if iface.Method(0).Name() == name {
			return implementsEither(typ, iface)
		}
	}
	for _, m := range jsontextMethods {
		if m.name == name {
			return hasJSONTextMethod(typ, m.name, m.param)
		}
	}
	return false
}

// omitsMethod reports whether the encoding method name of typ is omitted.
func (e *extractor) omitsMethod(typ types.Type, name string) bool {
	// A pointer type has the methods of its element type.
	typ, _ = derefPointer(types.Unalias(typ))
	typ = types.Unalias(typ)
	if named, ok := typ.(*types.Named); ok {
		typ = named.Origin()
	}
	return e.omittedMethods[omittedMethod{typ, name}]
}
