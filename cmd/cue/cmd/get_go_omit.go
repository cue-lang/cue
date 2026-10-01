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
	"errors"
	"fmt"
	"go/types"
	"iter"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// omitSelector is a selector given to the --omit flag of get go,
// written as pkg.Name, where each element is a glob as understood by [path.Match].
type omitSelector struct {
	text string

	// pkg is the glob for the package's import path if byPath is set,
	// or for its name otherwise.
	pkg    string
	byPath bool

	name string

	// pkgs holds the import paths of the packages declaring
	// what the selector matched.
	pkgs map[string]bool
}

func parseOmitSelector(text string) (omitSelector, error) {
	sel := omitSelector{text: text}
	if slash := strings.LastIndex(text, "/"); slash >= 0 {
		dot := strings.LastIndex(text[slash:], ".")
		if dot < 0 {
			return sel, errors.New("must be pkg.Name")
		}
		sel.pkg, sel.name, sel.byPath = text[:slash+dot], text[slash+dot+1:], true
	} else {
		var ok bool
		sel.pkg, sel.name, ok = strings.Cut(text, ".")
		if !ok {
			return sel, fmt.Errorf("must be qualified by a package, such as %q", "*."+text)
		}
		if strings.Contains(sel.name, ".") {
			return sel, errors.New("must be pkg.Name")
		}
	}
	what, valid := "a Go identifier", isIdentRune
	if sel.byPath {
		what, valid = "an import path", isImportPathRune
	}
	if err := checkGlob(sel.pkg, what, valid); err != nil {
		return sel, err
	}
	if err := checkGlob(sel.name, "a Go identifier", isIdentRune); err != nil {
		return sel, err
	}
	return sel, nil
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

// matches reports whether sel matches the declaration of obj.
func (sel *omitSelector) matches(obj types.Object) bool {
	pkg := obj.Pkg().Name()
	if sel.byPath {
		pkg = obj.Pkg().Path()
	}
	okPkg, _ := path.Match(sel.pkg, pkg)
	okName, _ := path.Match(sel.name, obj.Name())
	return okPkg && okName
}

// initOmissions parses the --omit selectors given as values, each of them
// comma-separated, and records the declarations they match as omitted.
// A selector which matches nothing is an error, as it is likely a mistake.
func (e *extractor) initOmissions(values []string) error {
	for _, value := range values {
		for text := range strings.SplitSeq(value, ",") {
			if text == "" {
				continue
			}
			sel, err := parseOmitSelector(text)
			if err != nil {
				return fmt.Errorf("invalid --%s selector %q: %v", flagOmit, text, err)
			}
			sel.pkgs = e.omitMatching(sel)
			if len(sel.pkgs) == 0 {
				return fmt.Errorf("--%s selector %q matches nothing", flagOmit, text)
			}
			e.omits = append(e.omits, sel)
		}
	}
	return nil
}

// omitMatching records the declarations matched by sel as omitted,
// returning the import paths of the packages declaring them.
func (e *extractor) omitMatching(sel omitSelector) map[string]bool {
	pkgs := make(map[string]bool)
	for obj := range e.genDecls() {
		if sel.matches(obj) {
			e.omitted[obj] = true
			pkgs[obj.Pkg().Path()] = true
		}
	}
	return pkgs
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
// which get go may generate: those given as arguments, held by
// [extractor.done] before any generation starts, and their
// dependencies outside the standard library.
func (e *extractor) genDecls() iter.Seq[types.Object] {
	return func(yield func(types.Object) bool) {
		for _, p := range e.allPkgs {
			if !e.done[p.PkgPath] && isStdPkg(p.PkgPath) {
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
