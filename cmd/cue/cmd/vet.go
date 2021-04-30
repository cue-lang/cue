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
	"golang.org/x/text/message"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
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
	TEXT       .txt  (validate a single string value)

To activate this mode, the non-cue files must be explicitly mentioned on the
command line. There must also be at least one CUE file to hold the constraints.

In this mode, each file will be verified against a CUE constraint. If the files
contain multiple objects (such as using --- in YAML), they will all be verified
individually.

By default, each file is checked against the root of the loaded CUE files.
The -d can be used to only verify files against the result of an expression
evaluated within the CUE files. This can be useful if the CUE files contain
a set of definitions to pick from.

Examples:

  # Check files against a CUE file:
  cue vet foo.yaml foo.cue

  # Check files against a particular expression
  cue vet translations/*.yaml foo.cue -d '#Translation'

If more than one expression is given, all must match all values.
`

func newVetCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "validate data",
		Long:  vetDoc,
		RunE:  mkRunE(c, doVet),
	}

	addOrphanFlags(cmd.Flags())
	addInjectionFlags(cmd.Flags(), false)

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete")

	return cmd
}

// doVet validates instances. There are two modes:
// - Only packages: vet all these packages
// - Data files: compare each data instance against a single package.
//
// It is invalid to have data files with other than exactly one package.
//
// TODO: allow unrooted schema, such as JSON schema to compare against
// other values.
func doVet(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{
		noMerge: true,
	})
	exitOnErr(cmd, err, true)

	// Go into a special vet mode if the user explicitly specified non-cue
	// files on the command line.
	// TODO: unify these two modes.
	if len(b.orphaned) > 0 {
		vetFiles(cmd, b)
		return nil
	}

	shown := false

	iter := b.instances()
	defer iter.close()
	for iter.scan() {
		v := iter.value()
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
			cue.Definitions(true),
			cue.Hidden(true),
		}
		w := cmd.Stderr()
		err := v.Validate(append(opt, cue.Concrete(concrete))...)
		if err != nil && !hasFlag {
			err = v.Validate(append(opt, cue.Concrete(false))...)
			if !shown && err == nil {
				shown = true
				p := message.NewPrinter(getLang())
				_, _ = p.Fprintln(w,
					"some instances are incomplete; use the -c flag to show errors or suppress this message")
			}
		}
		exitOnErr(cmd, err, false)
	}
	exitOnErr(cmd, iter.err(), true)
	return nil
}

func vetFiles(cmd *Command, b *buildPlan) {
	// Use -r type root, instead of -e

	if !b.encConfig.Schema.Exists() {
		exitOnErr(cmd, errors.New("data files specified without a schema"), true)
	}

	iter := b.instances()
	defer iter.close()
	for iter.scan() {
		v := iter.value()

		// Always concrete when checking against concrete files.
		err := v.Validate(cue.Concrete(true))
		exitOnErr(cmd, err, false)
	}
	exitOnErr(cmd, iter.err(), false)
}
