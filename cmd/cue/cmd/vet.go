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

const vetDoc = `The vet command validates CUE and other data files.
The command is silent when it succeeds; otherwise it reports any errors found.

By default, vet ensures that the result of validation is concrete
by reporting an error if any resulting regular fields have non-concrete values.
Use -c=false to not require concreteness, or -c to show these error messages.

vet can also validate non-CUE files in these file formats:

  Format       Extensions
	JSON       .json .jsonl .ndjson
	YAML       .yaml .yml
	TOML       .toml
	TEXT       .txt  (validate a single string value)

Data files with multiple values, such as YAML with --- document separators,
are validated one object at a time. Use --list to validate them as a list.

By default, each file is checked against the root of the loaded CUE.
Use the -d flag to select a schema at a particular expression instead.

Examples:

  # Check that a collection of CUE packages has no errors.
  cue vet -c=false ./...

  # Check against a schema at the root of a CUE file:
  cue vet -c foo.cue foo.yaml

  # Check against a schema from a registry:
  cue vet -c -d '#Workflow' cue.dev/x/githubactions@latest workflow.yml

The -d flag can be repeated to validate against multiple schemas at once.
`

func newVetCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "validate data",
		Long:  vetDoc,
		RunE:  mkRunE(c, doVet),
	}

	addOrphanFlags(cmd)
	addInjectionFlags(cmd)

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete, or set -c=false to allow incomplete values")

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
	if err != nil {
		return err
	}

	// Go into a special vet mode if the user explicitly specified non-cue
	// files on the command line.
	// TODO: unify these two modes.
	if len(b.orphaned) > 0 {
		return vetFiles(cmd, b)
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
					"some instances are incomplete; use the -c flag to show errors or -c=false to allow incomplete instances")
			}
		}
		printError(cmd, err)
	}
	if err := iter.err(); err != nil {
		return err
	}
	return nil
}

func vetFiles(cmd *Command, b *buildPlan) error {
	// Use -r type root, instead of -e

	if !b.encConfig.Schema.Exists() {
		return errors.New("data files specified without a schema")
	}

	iter := b.instances()
	defer iter.close()
	for iter.scan() {
		v := iter.value()

		// Always concrete when checking against concrete files.
		err := v.Validate(cue.Concrete(true))
		printError(cmd, err)
	}
	if err := iter.err(); err != nil {
		return err
	}
	return nil
}
