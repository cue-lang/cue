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
	"cuelang.org/go/cue/token"
	"github.com/spf13/cobra"
)

// vetCmd represents the vet command
var vetCmd = &cobra.Command{
	Use:   "vet",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: doVet,
}

func init() {
	rootCmd.AddCommand(vetCmd)
}

func doVet(cmd *cobra.Command, args []string) error {

	instances := buildFromArgs(cmd, args)

	var exprs []ast.Expr
	for _, e := range *expressions {
		expr, err := parser.ParseExpr(token.NewFileSet(), "<expression flag>", e)
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
		if *fVerbose || err != nil {
			printHeader(w, inst.Dir)
		}
		exitIfErr(cmd, inst, err, false)
	}
	return nil
}
