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

package gocode

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	"golang.org/x/tools/go/packages"
)

// Config defines options for generation Go code.
type Config struct {
	// Prefix is used as a prefix to all generated variables. It defaults to
	// cuegen.
	Prefix string

	// ValidateName defines the default name for validation methods or prefix
	// for validation functions. The default is "Validate". Set to "-" to
	// disable generating validators.
	ValidateName string

	// CompleteName defines the default name for complete methods or prefix
	// for complete functions. The default is "-" (disabled).
	CompleteName string

	// The cue.Runtime variable name to use for initializing Codecs.
	// A new Runtime is created by default.
	RuntimeVar string
}

const defaultPrefix = "cuegen"

// Generate generates Go code for the given instance in the directory of the
// given package.
//
// Generate converts top-level declarations to corresponding Go code. By default,
// it will only generate validation functions of methods for exported top-level
// declarations. The behavior can be altered with the @go attribute.
//
// The go attribute has the following form @go(<name>{,<option>}), where option
// is either a key-value pair or a flag. The name maps the CUE name to an
// alternative Go name. The special value '-' is used to indicate the field
// should be ignored for any Go generation.
//
// The following options are supported:
//
//     type=<gotype>    The Go type as which this value should be interpreted.
//                      This defaults to the type with the (possibly overridden)
//                      name of the field.
//     validate=<name>  Alternative name for the validation function or method
//                      Setting this to the empty string disables generation.
//     complete=<name>  Alternative name for the validation function or method.
//                      Setting this to the empty string disables generation.
//     func             Generate as a function instead of a method.
//
//
// Selection and Naming
//
// Generate will not generate any code for fields that have no go attribute
// and that are not exported or for which there is no namesake Go type.
// If the go attribute has the special value '-' as its name it wil be dropped
// as well. In all other cases Generate will generate Go code, even if the
// resulting code will not compile. For instance, Generate will generate Go
// code even if the user defines a Go type in the attribute that does not
// exist.
//
// If a field selected for generation and the go name matches that the name of
// the Go type, the corresponding validate and complete code are generated as
// methods by default. If not, it will be generated as a function. The default
// function name is the default operation name with the Go name as a suffix.
//
//
// Caveats
// Currently not supported:
//   - option to generate Go structs (or automatically generate if undefined)
//   - for type option to refer to types outside the package.
//
func Generate(pkgPath string, inst *cue.Instance, c *Config) (b []byte, err error) {
	// TODO: if inst is nil, the instance is loaded from CUE files in the same
	// package directory with the same package name.
	if err = inst.Value().Validate(); err != nil {
		return nil, err
	}
	if c == nil {
		c = &Config{}
	}
	g := &generator{
		Config: *c,

		typeMap: map[string]types.Type{},
	}

	pkgName := inst.Name

	if pkgPath != "" {
		loadCfg := &packages.Config{
			Mode: packages.LoadAllSyntax,
		}
		pkgs, err := packages.Load(loadCfg, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("generating failed: %v", err)
		}

		if len(pkgs) != 1 {
			return nil, fmt.Errorf(
				"generate only allowed for one package at a time, found %d",
				len(pkgs))
		}

		g.pkg = pkgs[0]
		if len(g.pkg.Errors) > 0 {
			for _, err := range g.pkg.Errors {
				g.addErr(err)
			}
			return nil, g.err
		}

		pkgName = g.pkg.Name

		for _, obj := range g.pkg.TypesInfo.Defs {
			if obj == nil || obj.Pkg() != g.pkg.Types {
				continue
			}
			g.typeMap[obj.Name()] = obj.Type()
		}
	}

	// TODO: add package doc if there is no existing Go package or if it doesn't
	// have package documentation already.
	g.exec(headerCode, map[string]string{
		"pkgName": pkgName,
	})

	iter, err := inst.Value().Fields()
	g.addErr(err)

	for iter.Next() {
		g.decl(iter.Label(), iter.Value())
	}

	r := internal.GetRuntime(inst).(*cue.Runtime)
	b, err = r.Marshal(inst)
	g.addErr(err)

	g.exec(loadCode, map[string]string{
		"runtime": g.RuntimeVar,
		"prefix":  strValue(g.Prefix, defaultPrefix),
		"data":    string(b),
	})

	if g.err != nil {
		return nil, g.err
	}

	b, err = format.Source(g.w.Bytes())
	if err != nil {
		// Return bytes as well to allow analysis of the failed Go code.
		return g.w.Bytes(), err
	}

	return b, err
}

type generator struct {
	Config
	pkg     *packages.Package
	typeMap map[string]types.Type

	w   bytes.Buffer
	err errors.Error
}

func (g *generator) addErr(err error) {
	if err != nil {
		g.err = errors.Append(g.err, errors.Promote(err, "generate failed"))
	}
}

func (g *generator) exec(t *template.Template, data interface{}) {
	g.addErr(t.Execute(&g.w, data))
}

func (g *generator) decl(name string, v cue.Value) {
	attr := v.Attribute("go")

	if !ast.IsExported(name) && attr.Err() != nil {
		return
	}

	goName := name
	switch s, _ := attr.String(0); s {
	case "":
	case "-":
		return
	default:
		goName = s
	}

	goTypeName := goName
	goType := ""
	if str, ok, _ := attr.Lookup(1, "type"); ok {
		goType = str
		goTypeName = str
	}

	isFunc, _ := attr.Flag(1, "func")
	if goTypeName != goName {
		isFunc = true
	}

	zero := "nil"

	typ := g.typeMap[goTypeName]
	if goType == "" {
		goType = goTypeName
		if typ != nil {
			switch typ.Underlying().(type) {
			case *types.Struct, *types.Array:
				goType = "*" + goTypeName
				zero = fmt.Sprintf("&%s{}", goTypeName)
			case *types.Pointer:
				zero = fmt.Sprintf("%s(nil)", goTypeName)
				isFunc = true
			}
		}
	}

	g.exec(stubCode, map[string]interface{}{
		"prefix":  strValue(g.Prefix, defaultPrefix),
		"cueName": name,   // the field name of the CUE type
		"goType":  goType, // the receiver or argument type
		"zero":    zero,   // the zero value of the underlying type

		// @go attribute options
		"func":     isFunc,
		"validate": lookupName(attr, "validate", strValue(g.ValidateName, "Validate")),
		"complete": lookupName(attr, "complete", g.CompleteName),
	})
}

func lookupName(attr cue.Attribute, option, config string) string {
	name, ok, _ := attr.Lookup(1, option)
	if !ok {
		name = config
	}
	if name == "-" {
		return ""
	}
	return name
}

func strValue(have, fallback string) string {
	if have == "" {
		return fallback
	}
	return have
}
