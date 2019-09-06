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
	"fmt"
	"io/ioutil"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/encoding"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"github.com/spf13/cobra"
	"golang.org/x/text/message"
)

const vetDoc = `vet validates CUE and other data files

By default it will only validate if there are no errors.
The -c validates that all regular fields are concrete.


Checking non-CUE files

Vet can also check non-CUE files. The following file formats are
currently supported:

  Format       Extensions
	JSON       .json .jsonl .ndjson
	YAML       .yaml .yml

To activate this mode, the non-cue files must be explicitly mentioned on the
command line. There must also be at least one CUE file to hold the constraints.

In this mode, each file will be verified against a CUE constraint. If the files
contain multiple objects (such as using --- in YAML), they will all be verified
individually.

By default, each file is checked against the root of the loaded CUE files.
The -e can be used to only verify files against the result of an expression
evaluated within the CUE files. This can be useful if the CUE files contain
a set of definitions to pick from.

Examples:

  # Check files against a CUE file:
  cue vet foo.yaml foo.cue

  # Check files against a particular expression
  cue vet translations/*.yaml foo.cue -e Translation

If more than one expression is given, all must match all values.
`

func newVetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "validate data",
		Long:  vetDoc,
		RunE:  doVet,
	}

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete")

	cmd.Flags().StringArrayP(string(flagExpression), "e", nil,
		"use this expression to validate non-CUE files")

	return cmd
}

func doVet(cmd *cobra.Command, args []string) error {
	builds := loadFromArgs(cmd, args)
	if builds == nil {
		return nil
	}
	instances := buildInstances(cmd, builds)

	// Go into a special vet mode if the user explicitly specified non-cue
	// files on the command line.
	for _, a := range args {
		enc := encoding.MapExtension(filepath.Ext(a))
		if enc != nil && enc.Name() != "cue" {
			vetFiles(cmd, instances[0], builds[0].DataFiles)
			return nil
		}
	}

	shown := false

	for _, inst := range instances {
		// TODO: use ImportPath or some other sanitized path.

		concrete := true
		hasFlag := false
		if flag := cmd.Flag(string(flagConcrete)); flag != nil {
			hasFlag = flag.Changed
			if hasFlag {
				concrete = flagConcrete.Bool(cmd)
			}
		}
		opt := []cue.Option{
			cue.Attributes(true),
			cue.Optional(true),
			cue.Hidden(true),
		}
		w := cmd.OutOrStderr()
		err := inst.Value().Validate(append(opt, cue.Concrete(concrete))...)
		if err != nil && !hasFlag {
			err = inst.Value().Validate(append(opt, cue.Concrete(false))...)
			if !shown && err == nil {
				shown = true
				p := message.NewPrinter(getLang())
				_, _ = p.Fprintln(w,
					"some instances are incomplete; use the -c flag to show errors or suppress this message")
			}
		}
		exitIfErr(cmd, inst, err, false)
	}
	return nil
}

func vetFiles(cmd *cobra.Command, inst *cue.Instance, files []string) {
	expressions := flagExpression.StringArray(cmd)

	var check cue.Value

	if len(expressions) == 0 {
		check = inst.Value()
	}

	for _, e := range expressions {
		expr, err := parser.ParseExpr("<expression flag>", e)
		exitIfErr(cmd, inst, err, true)

		v := inst.Eval(expr)
		exitIfErr(cmd, inst, v.Err(), true)
		check = check.Unify(v)
	}

	for _, f := range files {
		b, err := ioutil.ReadFile(f)
		exitIfErr(cmd, inst, err, true)

		ext := filepath.Ext(filepath.Ext(f))
		enc := encoding.MapExtension(ext)
		if enc == nil {
			exitIfErr(cmd, inst, fmt.Errorf("unrecognized extension %q", ext), true)
		}

		var exprs []ast.Expr
		switch enc.Name() {
		case "json":
			exprs, err = handleJSON(f, bytes.NewReader(b))
		case "yaml":
			exprs, err = handleYAML(f, bytes.NewReader(b))
		default:
			exitIfErr(cmd, inst, fmt.Errorf("vet does not support %q", enc.Name()), true)
		}
		exitIfErr(cmd, inst, err, true)

		r := internal.GetRuntime(inst).(*cue.Runtime)
		for _, expr := range exprs {
			body, err := r.CompileExpr(expr)
			exitIfErr(cmd, inst, err, false)
			v := body.Value().Unify(check)
			if err := v.Err(); err != nil {
				exitIfErr(cmd, inst, err, false)
			} else {
				// Always concrete when checking against concrete files.
				err = v.Validate(cue.Concrete(true))
				exitIfErr(cmd, inst, err, false)
			}
		}
	}
}
