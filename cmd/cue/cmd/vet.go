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
)

func newVetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "validate CUE configurations",
		RunE:  doVet,
	}
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

	w := cmd.OutOrStdout()

	for _, inst := range instances {
		// TODO: use ImportPath or some other sanitized path.
		opt := []cue.Option{
			cue.Attributes(true),
			cue.Optional(true),
			cue.Concrete(true),
			cue.Hidden(true),
		}
		err := inst.Value().Validate(opt...)
		if flagVerbose.Bool(cmd) || err != nil {
			printHeader(w, inst.Dir)
		}
		exitIfErr(cmd, inst, err, false)
	}
	return nil
}
