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
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/diff"
	"cuelang.org/go/tools/trim"
)

// TODO:
// - remove the limitations mentioned in the documentation
// - implement verification post-processing as extra safety

// newTrimCmd creates a trim command
func newTrimCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trim",
		Short: "remove superfluous fields",
		Long: `trim removes fields from structs that can be inferred from constraints

A field, struct, or list is removed if it is implied by a constraint, such
as from an optional field maching a required field, a list type value,
a comprehension or any other implied content. It will modify the files in place.


Limitations

Removal is on a best effort basis. Some caveats:
- Fields in implied content may refer to fields within the struct in which
  they are included, but are only resolved on a best-effort basis.
- Disjunctions that contain structs in implied content cannot be used to
  remove fields.
- There is currently no verification step: manual verification is required.

Examples:

	$ cat <<EOF > foo.cue
	light: [string]: {
		room:          string
		brightnessOff: *0.0 | >=0 & <=100.0
		brightnessOn:  *100.0 | >=0 & <=100.0
	}

	light: ceiling50: {
		room:          "MasterBedroom"
		brightnessOff: 0.0    // this line
		brightnessOn:  100.0  // and this line will be removed
	}
	EOF

	$ cue trim foo.cue
	$ cat foo.cue
	light: [string]: {
		room:          string
		brightnessOff: *0.0 | >=0 & <=100.0
		brightnessOn:  *100.0 | >=0 & <=100.0
	}

	light: ceiling50: {
		room: "MasterBedroom"
	}

It is guaranteed that the resulting files give the same output as before the
removal.
`,
		RunE: mkRunE(c, runTrim),
	}

	flagOut.Add(cmd)
	return cmd
}

func runTrim(cmd *Command, args []string) error {
	// TODO: Do something more fine-grained. Optional fields are mostly not
	// useful to consider as an optional field will almost never subsume
	// another value. However, an optional field may subsume and therefore
	// trigger the removal of another optional field.
	// For now this is the better approach: trimming is not 100% accurate,
	// and optional fields are just more likely to cause edge cases that may
	// block a removal.
	internal.DropOptional = true
	defer func() { internal.DropOptional = false }()

	binst := loadFromArgs(cmd, args, defaultConfig)
	if binst == nil {
		return nil
	}
	instances := buildInstances(cmd, binst)

	// dst := flagName("o").String(cmd)
	dst := flagOut.String(cmd)
	if dst != "" && dst != "-" {
		switch _, err := os.Stat(dst); {
		case os.IsNotExist(err):
		case err == nil:
		default:
			return fmt.Errorf("error creating file: %v", err)
		}
	}

	overlay := map[string]load.Source{}

	for i, inst := range binst {
		root := instances[i]
		err := trim.Files(inst.Files, root, &trim.Config{
			Trace: flagTrace.Bool(cmd),
		})
		if err != nil {
			return err
		}

		for _, f := range inst.Files {
			overlay[f.Filename] = load.FromFile(f)
		}

	}

	cfg := *defaultConfig
	cfg.Overlay = overlay
	tinsts := buildInstances(cmd, load.Instances(args, &cfg))
	if len(tinsts) != len(binst) {
		return errors.New("unexpected number of new instances")
	}
	if !flagIgnore.Bool(cmd) {
		for i, p := range instances {
			k, script := diff.Final.Diff(p.Value(), tinsts[i].Value())
			if k != diff.Identity {
				diff.Print(os.Stdout, script)
				fmt.Println("Aborting trim, output differs after trimming. This is a bug! Use -i to force trim.")
				fmt.Println("You can file a bug here: https://github.com/cuelang/cue/issues/new?assignees=&labels=NeedsInvestigation&template=bug_report.md&title=")
				os.Exit(1)
			}
		}
	}

	if flagDryrun.Bool(cmd) {
		return nil
	}

	for _, inst := range binst {
		for _, f := range inst.Files {
			filename := f.Filename

			opts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				opts = append(opts, format.Simplify())
			}

			b, err := format.Node(f, opts...)
			if err != nil {
				return fmt.Errorf("error formatting file: %v", err)
			}

			if dst == "-" {
				_, err := cmd.OutOrStdout().Write(b)
				if err != nil {
					return err
				}
				continue
			} else if dst != "" {
				filename = dst
			}

			err = ioutil.WriteFile(filename, b, 0644)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
