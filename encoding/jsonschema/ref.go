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
	"net/url"
	"path"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

func (d *decoder) parseRef(p token.Pos, str string) []string {
	u, err := url.Parse(str)
	if err != nil {
		d.addErr(errors.Newf(p, "invalid JSON reference: %s", err))
		return nil
	}

	if u.Host != "" || u.Path != "" {
		d.addErr(errors.Newf(p, "external references (%s) not supported", str))
		// TODO: handle
		//    host:
		//      If the host corresponds to a package known to cue,
		//      load it from there. It would prefer schema converted to
		//      CUE, although we could consider loading raw JSON schema
		//      if present.
		//      If not present, advise the user to run cue get.
		//    path:
		//      Look up on file system or relatively to authority location.
		return nil
	}

	if !path.IsAbs(u.Fragment) {
		d.addErr(errors.Newf(p, "anchors (%s) not supported", u.Fragment))
		// TODO: support anchors
		return nil
	}

	// NOTE: Go bug?: url.URL has no raw representation of the fragment. This
	// means that %2F gets translated to `/` before it can be split. This, in
	// turn, means that field names cannot have a `/` as name.

	s := strings.TrimRight(u.Fragment[1:], "/")
	return strings.Split(s, "/")
}

func (d *decoder) mapRef(p token.Pos, str string, ref []string) []ast.Label {
	fn := d.cfg.Map
	if fn == nil {
		fn = jsonSchemaRef
	}
	a, err := fn(p, ref)
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

func jsonSchemaRef(p token.Pos, a []string) ([]ast.Label, error) {
	// TODO: technically, references could reference a
	// non-definition. We disallow this case for the standard
	// JSON Schema interpretation. We could detect cases that
	// are not definitions and then resolve those as literal
	// values.
	if len(a) != 2 || (a[0] != "definitions" && a[0] != "$defs") {
		return nil, errors.Newf(p,
			// Don't mention the ability to use $defs, as this definition seems
			// to already have been withdrawn from the JSON Schema spec.
			"$ref must be of the form #/definitions/...")
	}
	name := a[1]
	if ast.IsValidIdent(name) &&
		name != rootDefs[1:] &&
		!internal.IsDefOrHidden(name) {
		return []ast.Label{ast.NewIdent("#" + name)}, nil
	}
	return []ast.Label{ast.NewIdent(rootDefs), ast.NewString(name)}, nil
}
