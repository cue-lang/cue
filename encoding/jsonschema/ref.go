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

package jsonschema

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

func parseRootRef(str string) (cue.Path, error) {
	u, err := url.Parse(str)
	if err != nil {
		return cue.Path{}, fmt.Errorf("invalid JSON reference: %s", err)
	}
	if u.Host != "" || u.Path != "" || u.Opaque != "" {
		return cue.Path{}, fmt.Errorf("external references (%s) not supported in Root", str)
	}
	if u.Fragment == "/" {
		// As a special case for backward compatibility, treat
		// `#/` as equivalent to `#` because the docs specifically
		// mention that it refers to the root document.
		// (technically the JSON Pointer `/` means a one-element path
		// with an empty element).
		return cue.Path{}, nil
	}
	fragmentParts, err := splitFragment(u)
	if err != nil {
		return cue.Path{}, err
	}
	var selectors []cue.Selector
	for _, r := range fragmentParts {
		// Technically this is incorrect because a numeric
		// element could also index into a list, but the
		// resulting CUE path will not allow that.
		selectors = append(selectors, cue.Str(r))
	}
	return cue.MakePath(selectors...), nil
}

var errRefNotFound = errors.New("JSON Pointer reference not found")

func lookupJSONPointer(v cue.Value, p string) (cue.Value, error) {
	parts, err := splitJSONPointer(p)
	if err != nil {
		return cue.Value{}, err
	}
	for _, part := range parts {
		// Note: a JSON Pointer doesn't distinguish between indexing
		// and struct lookup. We have to use the value itself to decide
		// which operation is appropriate.
		v, _ := v.Default()
		switch v.Kind() {
		case cue.StructKind:
			v = v.LookupPath(cue.MakePath(cue.Str(part)))
		case cue.ListKind:
			idx := int64(0)
			if len(part) > 1 && part[0] == '0' {
				// Leading zeros are not allowed
				return cue.Value{}, errRefNotFound
			}
			idx, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				return cue.Value{}, errRefNotFound
			}
			v = v.LookupPath(cue.MakePath(cue.Index(idx)))
		}
		if !v.Exists() {
			return cue.Value{}, errRefNotFound
		}
	}
	return v, nil
}

func sameSchemaRoot(u1, u2 *url.URL) bool {
	return u1.Host == u2.Host && u1.Path == u2.Path && u1.Fragment == u2.Fragment
}

// splitFragment splits the fragment part of a URI into path components
// and removes the fragment part from u.
// The result may be an empty slice.
func splitFragment(u *url.URL) ([]string, error) {
	frag := u.Fragment
	if frag == "" {
		return nil, nil
	}
	if !strings.HasPrefix(frag, "/") {
		return nil, fmt.Errorf("anchors (%s) not supported", frag)
	}
	u.Fragment = ""
	u.RawFragment = ""

	return splitJSONPointer(frag)
}

var (
	jsonPtrEsc   = strings.NewReplacer("~", "~0", "/", "~1")
	jsonPtrUnesc = strings.NewReplacer("~0", "~", "~1", "/")
)

func splitJSONPointer(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	if s[0] != '/' {
		return nil, fmt.Errorf("non-empty JSON pointer must start with /")
	}
	s = s[1:]
	parts := strings.Split(s, "/")
	if !strings.Contains(s, "~") {
		return parts, nil
	}
	for i, part := range parts {
		// TODO this leaves invalid escape sequences like
		// ~2 unchanged where we should probably return an
		// error.
		parts[i] = jsonPtrUnesc.Replace(part)
	}
	return parts, nil
}

// resolveURI parses a URI from s and resolves it in the current context.
// To resolve it in the current context, it looks for the closest URI from
// an $id in the parent scopes and the uses the URI resolution to get the
// new URI.
//
// This method is used to resolve any URI, including those from $id and $ref.
func (s *state) resolveURI(n cue.Value) *url.URL {
	str, ok := s.strValue(n)
	if !ok {
		return nil
	}

	u, err := url.Parse(str)
	if err != nil {
		s.addErr(errors.Newf(n.Pos(), "invalid JSON reference: %v", err))
		return nil
	}

	if u.IsAbs() {
		// Absolute URI: no need to walk up the tree.
		if u.Host == DefaultRootIDHost {
			// No-one should be using the default root ID explicitly.
			s.errf(n, "invalid use of default root ID host (%v) in URI", DefaultRootIDHost)
			return nil
		}
		return u
	}

	return s.schemaRoot().id.ResolveReference(u)
}

// schemaRoot returns the state for the nearest enclosing
// schema that has its own schema ID.
func (s *state) schemaRoot() *state {
	for ; s != nil; s = s.up {
		if s.id != nil {
			return s
		}
	}
	// Should never happen, as we ensure there's always an absolute
	// URI at the root.
	panic("unreachable")
}

// DefaultMapRef implements the default logic for mapping a schema location
// to CUE.
// It uses a heuristic to map the URL host and path to an import path,
// and maps the fragment part according to the following:
//
//	#                    <empty path>
//	#/definitions/foo   #foo or #."foo"
//	#/$defs/foo   #foo or #."foo"
func DefaultMapRef(loc SchemaLoc) (importPath string, path cue.Path, err error) {
	return defaultMapRef(loc, defaultMap, DefaultMapURL)
}

// defaultMapRef implements the default MapRef semantics
// in terms of the default Map and MapURL functions provided
// in the configuration.
func defaultMapRef(
	loc SchemaLoc,
	mapFn func(pos token.Pos, path []string) ([]ast.Label, error),
	mapURLFn func(u *url.URL) (importPath string, path cue.Path, err error),
) (importPath string, path cue.Path, err error) {
	var fragment string
	if loc.RootRel == nil {
		// It's external and mapURLFn is provided; use it.
		u := ref(*loc.ID)
		fragment = loc.ID.Fragment
		u.Fragment = ""
		var err error
		importPath, path, err = mapURLFn(u)
		if err != nil {
			return "", cue.Path{}, err
		}
	} else {
		fragment = loc.RootRel.Fragment
	}
	parts, err := splitJSONPointer(fragment)
	if err != nil {
		return "", cue.Path{}, err
	}
	labels, err := mapFn(token.Pos{}, parts)
	if err != nil {
		return "", cue.Path{}, err
	}
	relPath, err := labelsToCUEPath(labels)
	if err != nil {
		return "", cue.Path{}, err
	}
	return importPath, pathConcat(path, relPath), nil
}

func pathConcat(p1, p2 cue.Path) cue.Path {
	sels1, sels2 := p1.Selectors(), p2.Selectors()
	if len(sels1) == 0 {
		return p2
	}
	if len(sels2) == 0 {
		return p1
	}
	return cue.MakePath(append(slices.Clip(sels1), sels2...)...)
}

func labelsToCUEPath(labels []ast.Label) (cue.Path, error) {
	sels := make([]cue.Selector, len(labels))
	for i, label := range labels {
		// TODO this doesn't allow defining hidden labels. We should
		// probably use enhanced logic to allow that.
		sels[i] = cue.Label(label)
	}
	p := cue.MakePath(sels...)
	if err := p.Err(); err != nil {
		return cue.Path{}, err
	}
	return p, nil
}

func (d *decoder) mapRef(p token.Pos, str string, ref []string) []ast.Label {
	a, err := d.cfg.Map(p, ref)
	if err != nil {
		if str == "" {
			str = "#/" + strings.Join(ref, "/")
		}
		d.addErr(errors.Newf(p, "invalid reference %q: %v", str, err))
		return nil
	}
	if len(a) == 0 {
		// TODO: should we allow inserting at root level?
		if str == "" {
			str = "#/" + strings.Join(ref, "/")
		}
		d.addErr(errors.Newf(p,
			"invalid empty reference returned by map for %q", str))
		return nil
	}
	return a
}

func defaultMap(p token.Pos, a []string) ([]ast.Label, error) {
	// TODO: technically, references could reference a
	// non-definition. We disallow this case for the standard
	// JSON Schema interpretation. We could detect cases that
	// are not definitions and then resolve those as literal
	// values.
	if len(a) != 2 || (a[0] != "definitions" && a[0] != "$defs") {
		// It's an internal reference (or a nested definition reference).
		// Fall back to defining it in the internal namespace.
		// TODO this is needlessly inefficient, as we're putting something
		// back together that was already joined before defaultMap was
		// invoked. This does avoid dual implementations though.
		var buf strings.Builder
		for _, elem := range a {
			buf.WriteByte('/')
			buf.WriteString(jsonPtrEsc.Replace(elem))
		}
		return []ast.Label{ast.NewIdent("_#defs"), ast.NewString(buf.String())}, nil
	}
	name := a[1]
	if ast.IsValidIdent(name) &&
		name != rootDefs[1:] &&
		!internal.IsDefOrHidden(name) {
		return []ast.Label{ast.NewIdent("#" + name)}, nil
	}
	return []ast.Label{ast.NewIdent(rootDefs), ast.NewString(name)}, nil
}

// DefaultMapURL implements the default schema ID to import
// path mapping. It trims off any ".json" suffix and uses the
// package name "schema" if the final component of the path
// isn't a valid CUE identifier.
func DefaultMapURL(u *url.URL) (string, cue.Path, error) {
	p := u.Path
	base := path.Base(p)
	if !ast.IsValidIdent(base) {
		base = strings.TrimSuffix(base, ".json")
		if !ast.IsValidIdent(base) {
			// Find something more clever to do there. For now just
			// pick "schema" as the package name.
			base = "schema"
		}
		p += ":" + base
	}
	return u.Host + p, cue.Path{}, nil
}

// pathRefSyntax returns the syntax for an expression which
// looks up the path inside the given root expression's value.
// It returns an error if the path contains any elements with
// type [cue.OptionalConstraint], [cue.RequiredConstraint], or [cue.PatternConstraint],
// none of which are expressible as a CUE index expression.
//
// TODO implement this properly and move to a method on [cue.Path].
func pathRefSyntax(cuePath cue.Path, root ast.Expr) (ast.Expr, error) {
	expr := root
	for _, sel := range cuePath.Selectors() {
		if sel.LabelType() == cue.IndexLabel {
			expr = &ast.IndexExpr{
				X: expr,
				Index: &ast.BasicLit{
					Kind:  token.INT,
					Value: sel.String(),
				},
			}
		} else {
			lab, err := labelForSelector(sel)
			if err != nil {
				return nil, err
			}
			expr = &ast.SelectorExpr{
				X:   expr,
				Sel: lab,
			}
		}
	}
	return expr, nil
}

func labelForSelector(sel cue.Selector) (ast.Label, error) {
	switch sel.LabelType() {
	case cue.StringLabel, cue.DefinitionLabel, cue.HiddenLabel, cue.HiddenDefinitionLabel:
		str := sel.String()
		switch {
		case strings.HasPrefix(str, `"`):
			// It's quoted for a reason, so maintain the quotes.
			return &ast.BasicLit{
				Kind:  token.STRING,
				Value: str,
			}, nil
		case ast.IsValidIdent(str):
			return ast.NewIdent(str), nil
		}
		// Should never happen.
		return nil, fmt.Errorf("cannot form expression for selector %q", sel)
	default:
		return nil, fmt.Errorf("cannot form label for selector %q with type %v", sel, sel.LabelType())
	}
}

func cuePathToJSONPointer(p cue.Path) string {
	var buf strings.Builder
	for _, sel := range p.Selectors() {
		if sel.Type() != cue.StringLabel {
			panic(fmt.Errorf("cannot convert selector %v to JSON pointer", sel))
		}
		buf.WriteByte('/')
		buf.WriteString(jsonPtrEsc.Replace(sel.Unquoted()))
	}
	return buf.String()
}

// relPath returns the path to v relative to root,
// which must be a direct ancestor of v.
func relPath(v, root cue.Value) cue.Path {
	rootPath := root.Path().Selectors()
	vPath := v.Path().Selectors()
	if !sliceHasPrefix(vPath, rootPath) {
		panic("value is not inside root")
	}
	return cue.MakePath(vPath[len(rootPath):]...)
}

func sliceHasPrefix[E comparable](s1, s2 []E) bool {
	if len(s2) > len(s1) {
		return false
	}
	return slices.Equal(s1[:len(s2)], s2)
}
