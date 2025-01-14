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

package jsonschema

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Generic constraints

func constraintAddDefinitions(key string, n cue.Value, s *state) {
	if n.Kind() != cue.StructKind {
		s.errf(n, `%q expected an object, found %s`, key, n.Kind())
	}

	s.processMap(n, func(key string, n cue.Value) {
		// Ensure that we are going to make a definition
		// for this node.
		s.ensureDefinition(n)
		s.schema(n)
	})
}

func constraintComment(key string, n cue.Value, s *state) {
}

func constraintConst(key string, n cue.Value, s *state) {
	s.all.add(n, s.constValue(n))
	s.allowedTypes &= n.Kind()
	s.knownTypes &= n.Kind()
}

func constraintDefault(key string, n cue.Value, s *state) {
	// TODO make the default value available in a separate
	// template-like CUE value outside of the usual schema output.
}

func constraintDeprecated(key string, n cue.Value, s *state) {
	if s.boolValue(n) {
		s.deprecated = true
	}
}

func constraintDescription(key string, n cue.Value, s *state) {
	s.description, _ = s.strValue(n)
}

func constraintEnum(key string, n cue.Value, s *state) {
	var a []ast.Expr
	var types cue.Kind
	for _, x := range s.listItems("enum", n, true) {
		if (s.allowedTypes & x.Kind()) == 0 {
			// Enum value is redundant because it's
			// not in the allowed type set.
			continue
		}
		a = append(a, s.constValue(x))
		types |= x.Kind()
	}
	s.knownTypes &= types
	s.allowedTypes &= types
	if len(a) > 0 {
		s.all.add(n, ast.NewBinExpr(token.OR, a...))
	}
}

func constraintExamples(key string, n cue.Value, s *state) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of "examples" must be an array, found %v`, n.Kind())
	}
}

func constraintNullable(key string, n cue.Value, s *state) {
	null := ast.NewNull()
	setPos(null, n)
	s.nullable = null
}

func constraintRef(key string, n cue.Value, s *state) {
	u := s.resolveURI(n)
	if u == nil {
		return
	}
	schemaRoot := s.schemaRoot()
	if u.Fragment == "" && schemaRoot.isRoot && sameSchemaRoot(u, schemaRoot.id) {
		// It's a reference to the root of the schema being
		// generated. This never maps to something different.
		s.all.add(n, s.refExpr(n, "", cue.Path{}))
		return
	}
	importPath, path, err := cueLocationForRef(s, n, u, schemaRoot)
	if err != nil {
		s.errf(n, "%v", err)
		return
	}
	if e := s.refExpr(n, importPath, path); e != nil {
		s.all.add(n, e)
	}
}

func cueLocationForRef(s *state, n cue.Value, u *url.URL, schemaRoot *state) (importPath string, path cue.Path, err error) {
	if ds, ok := s.defs[u.String()]; ok {
		// We already know about the schema, so use the information that's stored for it.
		return ds.importPath, ds.path, nil
	}
	loc := SchemaLoc{
		ID: u,
	}
	var base cue.Value
	isAnchor := u.Fragment != "" && !strings.HasPrefix(u.Fragment, "/")
	if !isAnchor {
		// It's a JSON pointer reference.
		if sameSchemaRoot(u, s.rootID) {
			base = s.root
		} else if sameSchemaRoot(u, schemaRoot.id) {
			// it's within the current schema.
			base = schemaRoot.pos
		}
		if base.Exists() {
			target, err := lookupJSONPointer(schemaRoot.pos, u.Fragment)
			if err != nil {
				if errors.Is(err, errRefNotFound) {
					return "", cue.Path{}, fmt.Errorf("reference to non-existent schema")
				}
				return "", cue.Path{}, fmt.Errorf("invalid JSON Pointer: %v", err)
			}
			if ds := s.defForValue.get(target); ds != nil {
				// There's a definition in place for the value, which gives
				// us our answer.
				return ds.importPath, ds.path, nil
			}
			s.ensureDefinition(target)
			loc.IsLocal = true
			loc.Path = relPath(target, s.root)
		}
	}
	importPath, path, err = s.cfg.MapRef(loc)
	if err != nil {
		return "", cue.Path{}, fmt.Errorf("cannot determine CUE location for JSON Schema location %v: %v", loc, err)
	}
	// TODO we'd quite like to avoid invoking MapRef many times
	// for the same reference, but in general we don't necessily know
	// the canonical URI of the schema until we've done at least one pass.
	// There are potentially ways to do it, but leave it for now in favor
	// of simplicity.
	return importPath, path, nil
}

func constraintTitle(key string, n cue.Value, s *state) {
	s.title, _ = s.strValue(n)
}

func constraintType(key string, n cue.Value, s *state) {
	var types cue.Kind
	set := func(n cue.Value) {
		str, ok := s.strValue(n)
		if !ok {
			s.errf(n, "type value should be a string")
		}
		switch str {
		case "null":
			types |= cue.NullKind
			s.setTypeUsed(n, nullType)
			// TODO: handle OpenAPI restrictions.
		case "boolean":
			types |= cue.BoolKind
			s.setTypeUsed(n, boolType)
		case "string":
			types |= cue.StringKind
			s.setTypeUsed(n, stringType)
		case "number":
			types |= cue.NumberKind
			s.setTypeUsed(n, numType)
		case "integer":
			types |= cue.IntKind
			s.setTypeUsed(n, numType)
			s.add(n, numType, ast.NewIdent("int"))
		case "array":
			types |= cue.ListKind
			s.setTypeUsed(n, arrayType)
		case "object":
			types |= cue.StructKind
			s.setTypeUsed(n, objectType)

		default:
			s.errf(n, "unknown type %q", n)
		}
	}

	switch n.Kind() {
	case cue.StringKind:
		set(n)
	case cue.ListKind:
		for i, _ := n.List(); i.Next(); {
			set(i.Value())
		}
	default:
		s.errf(n, `value of "type" must be a string or list of strings`)
	}

	s.allowedTypes &= types
}
