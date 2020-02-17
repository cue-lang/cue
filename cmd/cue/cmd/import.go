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
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/protobuf"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/encoding"
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
	cmd.Flags().StringP(string(flagGlob), "n", "", "glob filter for file names")
	cmd.Flags().String(string(flagType), "", "only apply to files of this type")
	cmd.Flags().BoolP(string(flagForce), "f", false, "force overwriting existing files")
	cmd.Flags().Bool(string(flagDryrun), false, "only run simulation")

	cmd.Flags().StringArrayP(string(flagPath), "l", nil, "CUE expression for single path component")
	cmd.Flags().Bool(string(flagList), false, "concatenate multiple objects into a list")
	cmd.Flags().Bool(string(flagFiles), false, "split multiple entries into different files")
	cmd.Flags().BoolP(string(flagRecursive), "R", false, "recursively parse string values")
	cmd.Flags().Bool(string(flagWithContext), false, "import as object with contextual data")

	cmd.Flags().String("fix", "", "apply given fix")

	cmd.Flags().StringArrayP(string(flagProtoPath), "I", nil, "paths in which to search for imports")

	return cmd
}

const (
	flagFiles       flagName = "files"
	flagProtoPath   flagName = "proto_path"
	flagWithContext flagName = "with-context"
)

// TODO: factor out rooting of orphaned files.

func runImport(cmd *Command, args []string) (err error) {
	var group errgroup.Group

	pkgFlag := flagPackage.String(cmd)

	done := map[string]bool{}

	b, err := parseArgs(cmd, args, &load.Config{DataFiles: true})
	if err != nil {
		return err
	}
	builds := b.insts
	if b.orphanInstance != nil {
		builds = append(builds, b.orphanInstance)
	}
	for _, pkg := range builds {
		pkgName := pkgFlag
		if pkgName == "" {
			pkgName = pkg.PkgName
		}
		// TODO: allow if there is a unique package name.
		if pkgName == "" && len(builds) > 1 {
			err = fmt.Errorf("must specify package name with the -p flag")
			break
		}
		if err = pkg.Err; err != nil {
			break
		}
		if done[pkg.Dir] {
			continue
		}
		done[pkg.Dir] = true

		for _, f := range pkg.OrphanedFiles {
			f := f // capture range var
			group.Go(func() error { return handleFile(cmd, pkgName, f) })
		}
	}

	err2 := group.Wait()
	exitOnErr(cmd, err, true)
	exitOnErr(cmd, err2, true)
	return nil
}

func handleFile(cmd *Command, pkg string, file *build.File) (err error) {
	filename := file.Filename
	// filter file names
	re, err := regexp.Compile(flagGlob.String(cmd))
	if err != nil {
		return err
	}
	if !re.MatchString(filepath.Base(filename)) {
		return nil
	}

	// TODO: consider unifying the two modes.
	var objs []ast.Expr
	i := encoding.NewDecoder(file, &encoding.Config{
		Stdin:     stdin,
		Stdout:    stdout,
		ProtoPath: flagProtoPath.StringArray(cmd),
	})
	defer i.Close()
	for ; !i.Done(); i.Next() {
		if expr := i.Expr(); expr != nil {
			objs = append(objs, expr)
			continue
		}
		if err := processFile(cmd, i.File()); err != nil {
			return err
		}
	}

	if len(objs) > 0 {
		if err := processStream(cmd, pkg, filename, objs); err != nil {
			return err
		}
	}
	return i.Err()
}

func processFile(cmd *Command, file *ast.File) (err error) {
	name := file.Filename + ".cue"

	b, err := format.Node(file)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(name, b, 0644)
}

func processStream(cmd *Command, pkg, filename string, objs []ast.Expr) error {
	if flagFiles.Bool(cmd) {
		for i, f := range objs {
			err := combineExpressions(cmd, pkg, filename, i, f)
			if err != nil {
				return err
			}
		}
		return nil
	} else if len(objs) > 1 {
		if !flagList.Bool(cmd) && len(flagPath.StringArray(cmd)) == 0 && !flagFiles.Bool(cmd) {
			return fmt.Errorf("list, flag, or files flag needed to handle multiple objects in file %q", filename)
		}
	}
	return combineExpressions(cmd, pkg, filename, 0, objs...)
}

// TODO: implement a more fine-grained approach.
var mutex sync.Mutex

func combineExpressions(cmd *Command, pkg, filename string, idx int, objs ...ast.Expr) error {
	cueFile := newName(filename, idx)

	mutex.Lock()
	defer mutex.Unlock()

	if out := flagOut.String(cmd); out != "" {
		cueFile = out
	}
	if cueFile != "-" {
		switch _, err := os.Stat(cueFile); {
		case os.IsNotExist(err):
		case err == nil:
			if !flagForce.Bool(cmd) {
				// TODO: mimic old behavior: write to stderr, but do not exit
				// with error code. Consider what is best to do here.
				stderr := cmd.Command.OutOrStderr()
				fmt.Fprintf(stderr, "skipping file %q: already exists\n", cueFile)
				return nil
			}
		default:
			return fmt.Errorf("error creating file: %v", err)
		}
	}

	f, err := placeOrphans(cmd, filename, pkg, objs)
	if err != nil {
		return err
	}

	if flagRecursive.Bool(cmd) {
		h := hoister{fields: map[string]bool{}}
		h.hoist(f)
	}

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

func placeOrphans(cmd *Command, filename, pkg string, objs []ast.Expr) (*ast.File, error) {
	f := &ast.File{}

	index := newIndex()
	for i, expr := range objs {

		// Compute a path different from root.
		var pathElems []ast.Label

		switch {
		case len(flagPath.StringArray(cmd)) > 0:
			expr := expr
			if flagWithContext.Bool(cmd) {
				expr = ast.NewStruct(
					"data", expr,
					"filename", ast.NewString(filename),
					"index", ast.NewLit(token.INT, strconv.Itoa(i)),
					"recordCount", ast.NewLit(token.INT, strconv.Itoa(len(objs))),
				)
			}
			inst, err := runtime.CompileExpr(expr)
			if err != nil {
				return nil, err
			}

			for _, str := range flagPath.StringArray(cmd) {
				l, err := parser.ParseExpr("--path", str)
				if err != nil {
					return nil, fmt.Errorf(`labels are of form "cue import -l foo -l 'strings.ToLower(bar)'": %v`, err)
				}

				str, err := inst.Eval(l).String()
				if err != nil {
					return nil, fmt.Errorf("unsupported label path type: %v", err)
				}
				pathElems = append(pathElems, ast.NewString(str))
			}
		}

		if flagList.Bool(cmd) {
			idx := index
			for _, e := range pathElems {
				idx = idx.label(e)
			}
			if idx.field.Value == nil {
				idx.field.Value = &ast.ListLit{
					Lbrack: token.NoSpace.Pos(),
					Rbrack: token.NoSpace.Pos(),
				}
			}
			list := idx.field.Value.(*ast.ListLit)
			list.Elts = append(list.Elts, expr)
		} else if len(pathElems) == 0 {
			obj, ok := expr.(*ast.StructLit)
			if !ok {
				if _, ok := expr.(*ast.ListLit); ok {
					return nil, fmt.Errorf("expected struct as object root, did you mean to use the --list flag?")
				}
				return nil, fmt.Errorf("cannot map non-struct to object root")
			}
			f.Decls = append(f.Decls, obj.Elts...)
		} else {
			field := &ast.Field{Label: pathElems[0]}
			f.Decls = append(f.Decls, field)
			for _, e := range pathElems[1:] {
				newField := &ast.Field{Label: e}
				newVal := ast.NewStruct(newField)
				field.Value = newVal
				field = newField
			}
			field.Value = expr
		}
	}

	if pkg != "" {
		p := &ast.Package{Name: ast.NewIdent(pkg)}
		f.Decls = append([]ast.Decl{p}, f.Decls...)
	}

	if flagList.Bool(cmd) {
		switch x := index.field.Value.(type) {
		case *ast.StructLit:
			f.Decls = append(f.Decls, x.Elts...)
		case *ast.ListLit:
			f.Decls = append(f.Decls, &ast.EmbedDecl{Expr: x})
		default:
			panic("unreachable")
		}
	}

	return f, nil
}

type listIndex struct {
	index map[string]*listIndex
	field *ast.Field
}

func newIndex() *listIndex {
	return &listIndex{
		index: map[string]*listIndex{},
		field: &ast.Field{},
	}
}

func (x *listIndex) label(label ast.Label) *listIndex {
	key := internal.DebugStr(label)
	idx := x.index[key]
	if idx == nil {
		if x.field.Value == nil {
			x.field.Value = &ast.StructLit{}
		}
		obj := x.field.Value.(*ast.StructLit)
		newField := &ast.Field{Label: label}
		obj.Elts = append(obj.Elts, newField)
		idx = &listIndex{
			index: map[string]*listIndex{},
			field: newField,
		}
		x.index[key] = idx
	}
	return idx
}

func newName(filename string, i int) string {
	ext := filepath.Ext(filename)
	filename = filename[:len(filename)-len(ext)]
	if i > 0 {
		filename += fmt.Sprintf("-%d", i)
	}
	filename += ".cue"
	return filename
}

func handleProtoDef(cmd *Command, path string, r io.Reader) (f *ast.File, err error) {
	return protobuf.Extract(path, r, &protobuf.Config{Paths: flagProtoPath.StringArray(cmd)})
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
