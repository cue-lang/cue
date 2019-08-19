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
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"github.com/spf13/cobra"
	"golang.org/x/text/message"
)

func newVetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "validate CUE configurations",
		RunE:  doVet,
	}

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete")

	return cmd
}

func doVet(cmd *cobra.Command, args []string) error {
	instances := buildFromArgs(cmd, args)

	var exprs []ast.Expr
	for _, e := range flagExpression.StringArray(cmd) {
		expr, err := parser.ParseExpr("<expression flag>", e)
		if err != nil {
			return err
		}
		exprs = append(exprs, expr)
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
