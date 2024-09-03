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

package cmd

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/protobuf"
	"cuelang.org/go/internal"
	pkgyaml "cuelang.org/go/pkg/encoding/yaml"
)

func newImportCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [mode] [inputs]",
		Short: "convert other formats to CUE files",
		Long: `import converts other formats, like JSON and YAML to CUE files

Files can either be specified explicitly, or inferred from the
specified packages. Within packages, import only looks for JSON
and YAML files by default (see the "filetypes" help topic for
more info). This behavior can be overridden by specifying one of
the following modes:

   Mode       Extensions
   json       Look for JSON files (.json .jsonl .ndjson).
   yaml       Look for YAML files (.yaml .yml).
   toml       Look for TOML files (.toml).
   text       Look for text files (.txt).
   binary     Look for files with extensions specified by --ext
              and interpret them as binary.
   jsonschema Interpret JSON, YAML or CUE files as JSON Schema.
   openapi    Interpret JSON, YAML or CUE files as OpenAPI.
   auto       Look for JSON or YAML files and interpret them as
              data, JSON Schema, or OpenAPI, depending on
              existing fields.
   data       Look for JSON or YAML files and interpret them
              as data.
   proto      Convert Protocol buffer definition files and
              transitive dependencies.

Using the --ext flag in combination with a mode causes matched files to be
interpreted as the format indicated by the mode, overriding any other meaning
attributed to that extension.

auto mode

In auto mode, data files are interpreted based on some marker
fields. JSON Schema is identified by a top-level "$schema" field
with a URL of the form "https?://json-schema.org/.*schema#?".
OpenAPI is identified by the existence of a top-level field
"openapi", which must have a major semantic version of 3, and
the info.title and info.version fields.


proto mode

Proto mode converts .proto files containing Prototcol Buffer
definitions to CUE. The -I defines the path for includes. The
module root is added implicitly if it exists.

The package name for a converted file is derived from the
go_package option. It can be overridden with the -p flag.

A module root must be specified if a .proto files includes other
files within the module. Files include from outside the module
are also imported and stored within the cue.mod directory. The
import path is defined by either the go_package option or, in the
absence of this option, the googleapis.com/<proto package>
convention.

The following command imports all .proto files in all
subdirectories as well all dependencies.

   cue import proto -I ../include ./...

The module root is implicitly added as an import path.


Binary mode

Loads matched files as binary.


JSON/YAML mode

The -f option allows overwriting of existing files. This only
applies to files generated for explicitly specified files or
files contained in explicitly specified packages.

Use the -R option in addition to overwrite files generated for
transitive dependencies (files written to cue.mod/gen/...).

The -n option is a regexp used to filter file names in the
matched package directories.

The -I flag is used to specify import paths for proto mode.
The module root is implicitly added as an import if it exists.

Examples:

  # Convert individual files:
  $ cue import foo.json bar.json  # create foo.cue and bar.cue

  # Convert all json files in the indicated directories:
  $ cue import json ./...

The "flags" help topic describes how to assign values to a
specific path within a CUE namespace. Some examples of that

Examples:

  $ cat <<EOF > foo.yaml
  kind: Service
  name: booster
  EOF

  # include the parsed file as an emit value:
  $ cue import foo.yaml
  $ cat foo.cue
  {
      kind: Service
      name: booster
  }

  # include the parsed file at the root of the CUE file:
  $ cue import -f foo.yaml
  $ cat foo.cue
  kind: Service
  name: booster

  # include the import config at the mystuff path
  $ cue import -f -l '"mystuff"' foo.yaml
  $ cat foo.cue
  myStuff: {
      kind: Service
      name: booster
  }

  # append another object to the input file
  $ cat <<EOF >> foo.yaml
  ---
  kind: Deployment
  name: booster
  replicas: 1
  EOF

  # base the path values on the input
  $ cue import -f -l 'strings.ToLower(kind)' -l name foo.yaml
  $ cat foo.cue
  service: booster: {
      kind: "Service"
      name: "booster"
  }

  # base the path values on the input and file name
  $ cue import -f --with-context -l 'path.Base(filename)' -l data.kind foo.yaml
  $ cat foo.cue
  "foo.yaml": Service: {
      kind: "Service"
      name: "booster"
  }

  "foo.yaml": Deployment: {
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }

  # include all files as list elements
  $ cue import -f --list foo.yaml
  $ cat foo.cue
  [{
      kind: "Service"
      name: "booster"
  }, {
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }]

  # collate files with the same path into a list
  $ cue import -f -list -l 'strings.ToLower(kind)' foo.yaml
  $ cat foo.cue
  service: [{
      kind: "Service"
      name: "booster"
  }
  deployment: [{
      kind:     "Deployment"
      name:     "booster
      replicas: 1
  }]


Embedded data files

The --recursive or -R flag enables the parsing of fields that are string
representations of data formats themselves. A field that can be parsed is
replaced with a call encoding the data from a structured form that is placed
in a sibling field.

It is also possible to recursively hoist data formats:

Example:
  $ cat <<EOF > example.json
  "a": {
      "data": '{ "foo": 1, "bar": 2 }',
  }
  EOF

  $ cue import -R example.json
  $ cat example.cue
  import "encoding/json"

  a: {
      data: json.Encode(_data),
      _data = {
          foo: 1
          bar: 2
      }
  }
`,
		RunE: mkRunE(c, runImport),
	}

	addOutFlags(cmd.Flags(), false)
	addOrphanFlags(cmd.Flags())

	cmd.Flags().Bool(string(flagFiles), false, "split multiple entries into different files")
	cmd.Flags().Bool(string(flagDryRun), false, "show what files would be created")
	cmd.Flags().BoolP(string(flagRecursive), "R", false, "recursively parse string values")
	cmd.Flags().StringArray(string(flagExt), nil, "match files with these extensions")

	return cmd
}

// TODO: factor out rooting of orphaned files.

func runImport(cmd *Command, args []string) (err error) {
	c := &config{
		fileFilter:     `\.(json|yaml|yml|toml|jsonl|ndjson|ldjson)$`,
		interpretation: build.Auto,
		loadCfg:        &load.Config{DataFiles: true},
	}

	var mode string
	extensions := flagExt.StringArray(cmd)
	if len(args) >= 1 && !strings.ContainsAny(args[0], `/\:.`) {
		c.interpretation = ""
		if len(extensions) > 0 {
			c.overrideDefault = true
		}

		mode = args[0]
		args = args[1:]
		c.encoding = build.Encoding(mode)
		switch mode {
		case "proto":
			c.fileFilter = `\.proto$`
		case "json":
			c.fileFilter = `\.(json|jsonl|ndjson|ldjson)$`
		case "yaml":
			c.fileFilter = `\.(yaml|yml)$`
		case "toml":
			c.fileFilter = `\.toml$`
		case "text":
			c.fileFilter = `\.txt$`
		case "binary":
			if len(extensions) == 0 {
				return errors.Newf(token.NoPos,
					"use of --ext flag required in binary mode")
			}
		case "auto", "openapi", "jsonschema":
			c.interpretation = build.Interpretation(mode)
			c.encoding = "yaml"
		case "data":
			// default mode for encoding/ no interpretation.
			c.encoding = ""
		default:
			return errors.Newf(token.NoPos, "unknown mode %q", mode)
		}
	}
	if len(extensions) > 0 {
		c.fileFilter = `\.(` + strings.Join(extensions, "|") + `)$`
	}

	b, err := parseArgs(cmd, args, c)
	if err != nil {
		return err
	}

	switch mode {
	default:
		err = genericMode(cmd, b)
	case "proto":
		err = protoMode(b)
	}
	return err
}

func protoMode(b *buildPlan) error {
	var prev *build.Instance
	root := ""
	module := ""
	protoFiles := []*build.File{}

	for _, b := range b.insts {
		hasProto := false
		for _, f := range b.OrphanedFiles {
			if f.Encoding == "proto" {
				protoFiles = append(protoFiles, f)
				hasProto = true
			}
		}
		if !hasProto {
			continue
		}

		// check dirs, all must have same root.
		switch {
		case root != "":
			if b.Root != "" && root != b.Root {
				return errors.Newf(token.NoPos,
					"instances must have same root in proto mode; "+
						"found %q (%s) and %q (%s)",
					prev.Root, prev.DisplayPath, b.Root, b.DisplayPath)
			}
		case b.Root != "":
			root = b.Root
			module = b.Module
			prev = b
		}
	}

	c := &protobuf.Config{
		Root:     root,
		Module:   module,
		Paths:    b.encConfig.ProtoPath,
		PkgName:  b.encConfig.PkgName,
		EnumMode: flagProtoEnum.String(b.cmd),
	}
	if module != "" {
		// We only allow imports from packages within the module if an actual
		// module is allowed.
		c.Paths = append([]string{root}, c.Paths...)
	}
	p := protobuf.NewExtractor(c)
	for _, f := range protoFiles {
		_ = p.AddFile(f.Filename, f.Source)
	}

	files, err := p.Files()
	if err != nil {
		return err
	}

	modDir := ""
	if root != "" {
		modDir = internal.GenPath(root)
	}

	for _, f := range files {
		// Only write the cue.mod files if they don't exist or if -Rf is used.
		abs := f.Filename
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		force := flagForce.Bool(b.cmd)
		if flagRecursive.Bool(b.cmd) && strings.HasPrefix(abs, modDir) {
			force = false
		}

		cueFile, err := getFilename(b, f, root, force)
		if cueFile == "" {
			return err
		}
		err = writeFile(b, f, cueFile)
		if err != nil {
			return err
		}
	}
	return nil
}

func genericMode(cmd *Command, b *buildPlan) error {
	pkgFlag := flagPackage.String(cmd)
	for _, pkg := range b.insts {
		pkgName := cmp.Or(pkgFlag, pkg.PkgName)
		// TODO: allow if there is a unique package name.
		if pkgName == "" && len(b.insts) > 1 {
			return fmt.Errorf("must specify package name with the -p flag")
		}
	}

	for _, f := range b.imported {
		err := handleFile(b, f)
		if err != nil {
			return err
		}
	}
	return nil
}

func getFilename(b *buildPlan, f *ast.File, root string, force bool) (filename string, err error) {
	cueFile := cmp.Or(flagOutFile.String(b.cmd), f.Filename)

	if cueFile != "-" {
		switch _, err := os.Stat(cueFile); {
		case os.IsNotExist(err):
		case err == nil:
			if !force {
				// TODO: mimic old behavior: write to stderr, but do not exit
				// with error code. Consider what is best to do here.
				stderr := b.cmd.OutOrStderr()
				if root != "" {
					cueFile, _ = filepath.Rel(root, cueFile)
				}
				fmt.Fprintf(stderr, "Skipping file %q: already exists.\n",
					filepath.ToSlash(cueFile))
				if strings.HasPrefix(cueFile, "cue.mod") {
					fmt.Fprintln(stderr, "Use -Rf to override.")
				} else {
					fmt.Fprintln(stderr, "Use -f to override.")
				}
				return "", nil
			}
		default:
			return "", fmt.Errorf("error creating file: %v", err)
		}
	}
	return cueFile, nil
}

func handleFile(b *buildPlan, f *ast.File) (err error) {
	// TODO: fill out root.
	cueFile, err := getFilename(b, f, "", flagForce.Bool(b.cmd))
	if cueFile == "" {
		return err
	}

	if flagRecursive.Bool(b.cmd) {
		h := hoister{fields: map[string]bool{}}
		h.hoist(f)
	}

	return writeFile(b, f, cueFile)
}

func writeFile(p *buildPlan, f *ast.File, cueFile string) error {
	if flagDryRun.Bool(p.cmd) {
		cueFile, err := filepath.Rel(rootWorkingDir, cueFile)
		if err != nil {
			return err
		}
		stderr := p.cmd.OutOrStderr()
		fmt.Fprintf(stderr, "importing into %s\n", cueFile)
		return nil
	}
	b, err := format.Node(f, format.Simplify())
	if err != nil {
		return fmt.Errorf("error formatting file: %v", err)
	}

	if cueFile == "-" {
		_, err := p.cmd.OutOrStdout().Write(b)
		return err
	}
	_ = os.MkdirAll(filepath.Dir(cueFile), 0777)
	return os.WriteFile(cueFile, b, 0666)
}

type hoister struct {
	fields map[string]bool
}

func (h *hoister) hoist(f *ast.File) {
	ast.Walk(f, nil, func(n ast.Node) {
		name := ""
		switch x := n.(type) {
		case *ast.Field:
			name, _, _ = ast.LabelName(x.Label)
		case *ast.Alias:
			name = x.Ident.Name
		case *ast.LetClause:
			name = x.Ident.Name
		}
		if name != "" {
			h.fields[name] = true
		}
	})

	_ = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n.(type) {
		case *ast.Comprehension:
			return false
		}
		return true

	}, func(c astutil.Cursor) bool {
		switch f := c.Node().(type) {
		case *ast.Field:
			name, _, _ := ast.LabelName(f.Label)
			if name == "" {
				return false
			}

			lit, ok := f.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return false
			}

			str, err := literal.Unquote(lit.Value)
			if err != nil {
				return false
			}

			expr, enc := tryParse(str)
			if expr == nil {
				return false
			}

			importIdent := &ast.Ident{
				Name: enc,
				Node: ast.NewImport(nil, "encoding/"+enc),
			}

			// found a replaceable string
			dataField := h.uniqueName(name, "_", "cue_")

			f.Value = ast.NewCall(ast.NewSel(importIdent, "Marshal"), ast.NewIdent(dataField))

			// TODO: use definitions instead
			c.InsertAfter(astutil.ApplyRecursively(&ast.LetClause{
				Ident: ast.NewIdent(dataField),
				Expr:  expr,
			}))
		}
		return true
	})
	astutil.Sanitize(f)
}

func tryParse(str string) (s ast.Expr, pkg string) {
	b := []byte(str)
	if json.Valid(b) {
		expr, err := parser.ParseExpr("", b)
		if err != nil {
			// TODO: report error
			return nil, ""
		}
		switch expr.(type) {
		case *ast.StructLit, *ast.ListLit:
		default:
			return nil, ""
		}
		return expr, "json"
	}
	// When a string has no newlines, never treat it as
	// YAML because there's too much risk of false positives
	// with regular-expressions or other such syntax.
	// See issue 1443.
	if bytes.IndexByte(b, '\n') == -1 {
		return nil, ""
	}

	// TODO(mvdan): move from pkg/encoding/yaml to encoding/yaml.
	if expr, err := pkgyaml.Unmarshal(b); err == nil {
		switch expr.(type) {
		case *ast.StructLit, *ast.ListLit:
		default:
			return nil, ""
		}
		return expr, "yaml"
	}

	return nil, ""
}

func (h *hoister) uniqueName(base, prefix, typ string) string {
	base = strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.L, unicode.N) {
			return r
		}
		return '_'
	}, base)

	name := prefix + typ + base
	for {
		if !h.fields[name] {
			h.fields[name] = true
			return name
		}
		name = prefix + typ + base
		typ += "x"
	}
}
