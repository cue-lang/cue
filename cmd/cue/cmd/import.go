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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/internal/third_party/yaml"
)

func newImportCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "convert other data formats to CUE files",
		Long: `import converts other data formats, like JSON and YAML to CUE files

The following file formats are currently supported:

  Format       Extensions
	JSON       .json .jsonl .ndjson
	YAML       .yaml .yml
	protobuf   .proto

Files can either be specified explicitly, or inferred from the specified
packages. In either case, the file extension is replaced with .cue. It will
fail if the file already exists by default. The -f flag overrides this.

Examples:

  # Convert individual files:
  $ cue import foo.json bar.json  # create foo.yaml and bar.yaml

  # Convert all json files in the indicated directories:
  $ cue import ./... -type=json


The --path flag

By default the parsed files are included as emit values. This default can be
overridden by specifying a sequence of labels as you would in a CUE file.
An identifier or string label are interpreted as usual. A label expression is
evaluated within the context of the imported file. label expressions may also
refer to builtin packages, which will be implicitly imported.

The --with-context flag can be used to evaluate the label expression within
a struct with the following fields:

{
	// data holds the original source data
	// (perhaps one of several records in a file).
	data: _
	// filename holds the full path to the file.
	filename: string
	// index holds the 0-based index element of the
	// record within the file. For files containing only
	// one record, this will be 0.
	index: uint & <recordCount
	// recordCount holds the total number of records
	// within the file.
	recordCount: int & >=1
}

Handling multiple documents or streams

To handle Multi-document files, such as concatenated JSON objects or
YAML files with document separators (---) the user must specify either the
--path, --list, or --files flag. The -path flag assign each element to a path
(identical paths are treated as usual); -list concatenates the entries, and
--files causes each entry to be written to a different file. The -files flag
may only be used if files are explicitly imported. The --list flag may be
used in combination with the -path flag, concatenating each entry to the
mapped location.


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
  $ cue import -f -list -foo.yaml
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

	flagOut.Add(cmd)

	addOrphanFlags(cmd.Flags())

	cmd.Flags().String(string(flagType), "", "only apply to files of this type")
	cmd.Flags().BoolP(string(flagForce), "f", false, "force overwriting existing files")
	cmd.Flags().Bool(string(flagDryrun), false, "only run simulation")
	cmd.Flags().BoolP(string(flagRecursive), "R", false, "recursively parse string values")
	cmd.Flags().String("fix", "", "apply given fix") // XXX

	return cmd
}

const (
	flagFiles       flagName = "files"
	flagProtoPath   flagName = "proto_path"
	flagWithContext flagName = "with-context"
)

// TODO: factor out rooting of orphaned files.

func runImport(cmd *Command, args []string) (err error) {
	b, err := parseArgs(cmd, args, &load.Config{DataFiles: true})
	if err != nil {
		return err
	}

	pkgFlag := flagPackage.String(cmd)
	for _, pkg := range b.insts {
		pkgName := pkgFlag
		if pkgName == "" {
			pkgName = pkg.PkgName
		}
		// TODO: allow if there is a unique package name.
		if pkgName == "" && len(b.insts) > 1 {
			err = fmt.Errorf("must specify package name with the -p flag")
			exitOnErr(cmd, err, true)
		}
	}

	for _, f := range b.imported {
		err := handleFile(b, f)
		if err != nil {
			return err
		}
	}

	exitOnErr(cmd, err, true)
	return nil
}

func handleFile(b *buildPlan, f *ast.File) (err error) {
	cueFile := f.Filename
	if out := flagOut.String(b.cmd); out != "" {
		cueFile = out
	}
	if cueFile != "-" {
		switch _, err := os.Stat(cueFile); {
		case os.IsNotExist(err):
		case err == nil:
			if !flagForce.Bool(b.cmd) {
				// TODO: mimic old behavior: write to stderr, but do not exit
				// with error code. Consider what is best to do here.
				stderr := b.cmd.Command.OutOrStderr()
				fmt.Fprintf(stderr, "skipping file %q: already exists\n", cueFile)
				return nil
			}
		default:
			return fmt.Errorf("error creating file: %v", err)
		}
	}

	if flagRecursive.Bool(b.cmd) {
		h := hoister{fields: map[string]bool{}}
		h.hoist(f)
	}

	return writeFile(b.cmd, f, cueFile)
}

func writeFile(cmd *Command, f *ast.File, cueFile string) error {
	b, err := format.Node(f, format.Simplify())
	if err != nil {
		return fmt.Errorf("error formatting file: %v", err)
	}

	if cueFile == "-" {
		_, err := cmd.OutOrStdout().Write(b)
		return err
	}
	return ioutil.WriteFile(cueFile, b, 0644)
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

			pkg := c.Import("encoding/" + enc)
			if pkg == nil {
				return false
			}

			// found a replacable string
			dataField := h.uniqueName(name, "_", "cue_")

			f.Value = ast.NewCall(
				ast.NewSel(pkg, "Marshal"),
				ast.NewIdent(dataField))

			// TODO: use definitions instead
			c.InsertAfter(astutil.ApplyRecursively(&ast.Alias{
				Ident: ast.NewIdent(dataField),
				Expr:  expr,
			}))
		}
		return true
	})
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

	if expr, err := yaml.Unmarshal("", b); err == nil {
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
