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
	"github.com/spf13/cobra"

	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// newDefCmd creates a new eval command
func newDefCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "def",
		Short: "print consolidated definitions",
		Long: `def prints consolidated configuration as a single file.

Printing is skipped if validation fails.

The --expression flag is used to only print parts of a configuration.
`,
		RunE: mkRunE(c, runDef),
	}

	addOutFlags(cmd.Flags(), true)
	addOrphanFlags(cmd.Flags())
	addInjectionFlags(cmd.Flags(), false)

	cmd.Flags().StringArrayP(string(flagExpression), "e", nil, "evaluate this expression only")

	cmd.Flags().BoolP(string(flagAttributes), "A", false,
		"display field attributes")

	cmd.Flags().Bool(string(flagInlineImports), false,
		"expand references to non-core imports")

	// TODO: Option to include comments in output.
	return cmd
}

func runDef(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{mode: filetypes.Def})
	if err != nil {
		return err
	}

	e, err := encoding.NewEncoder(cmd.ctx, b.outFile, b.encConfig)
	if err != nil {
		return err
	}

	iter := b.instances()
	defer iter.close()
	for iter.scan() {
		var err error
		if f := iter.file(); f != nil {
			err = e.EncodeFile(f)
		} else {
			err = e.Encode(iter.value())
		}
		if err != nil {
			return err
		}
	}
	if err := iter.err(); err != nil {
		return err
	}

	if err := e.Close(); err != nil {
		return err
	}
	return nil
}
