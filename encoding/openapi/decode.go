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

package openapi

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal"
)

// Extract converts OpenAPI definitions to an equivalent CUE representation.
//
// It currently only converts entries in #/components/schema and extracts some
// meta data.
func Extract(data *cue.Instance, c *Config) (*ast.File, error) {
	// TODO: find a good OpenAPI validator. Both go-openapi and kin-openapi
	// seem outdated. The k8s one might be good, but avoid pulling in massive
	// amounts of dependencies.

	f := &ast.File{}
	add := func(d ast.Decl) {
		if d != nil {
			f.Decls = append(f.Decls, d)
		}
	}

	js, err := jsonschema.Extract(data, &jsonschema.Config{
		Root: oapiSchemas,
		Map:  openAPIMapping,
	})
	if err != nil {
		return nil, err
	}

	v := data.Value()

	doc, _ := v.Lookup("info", "title").String() // Required
	if s, _ := v.Lookup("info", "description").String(); s != "" {
		doc += "\n\n" + s
	}
	cg := internal.NewComment(true, doc)

	if c.PkgName != "" {
		p := &ast.Package{Name: ast.NewIdent(c.PkgName)}
		p.AddComment(cg)
		add(p)
	} else if cg != nil {
		add(cg)
	}

	i := 0
	for ; i < len(js.Decls); i++ {
		switch x := js.Decls[i].(type) {
		case *ast.Package:
			return nil, errors.Newf(x.Pos(), "unexpected package %q", x.Name.Name)

		case *ast.ImportDecl, *ast.CommentGroup:
			add(x)
			continue
		}
		break
	}

	// TODO: allow attributes before imports? Would be easier.

	// TODO: do we want to store the OpenAPI version?
	// if version, _ := v.Lookup("openapi").String(); version != "" {
	// 	add(internal.NewAttr("openapi", "version="+ version))
	// }

	info := v.Lookup("info")
	if version, _ := info.Lookup("version").String(); version != "" {
		add(internal.NewAttr("version", version))
	}

	add(fieldsAttr(info, "license", "name", "url"))
	add(fieldsAttr(info, "contact", "name", "url", "email"))
	// TODO: terms of service.

	if i < len(js.Decls) {
		ast.SetRelPos(js.Decls[i], token.NewSection)
		f.Decls = append(f.Decls, js.Decls[i:]...)
	}

	return f, nil
}

func fieldsAttr(v cue.Value, name string, fields ...string) ast.Decl {
	group := v.Lookup(name)
	if !group.Exists() {
		return nil
	}

	buf := &strings.Builder{}
	for _, f := range fields {
		if s, _ := group.Lookup(f).String(); s != "" {
			if buf.Len() > 0 {
				buf.WriteByte(',')
			}
			_, _ = fmt.Fprintf(buf, "%s=%q", f, s)
		}
	}
	return internal.NewAttr(name, buf.String())
}

const oapiSchemas = "#/components/schemas/"

func openAPIMapping(pos token.Pos, a []string) ([]string, error) {
	if len(a) != 3 || a[0] != "components" || a[1] != "schemas" {
		return nil, errors.Newf(pos,
			`openapi: reference must be of the form %q; found "#/%s"`,
			oapiSchemas, strings.Join(a, "/"))
	}
	return a[2:], nil
}
