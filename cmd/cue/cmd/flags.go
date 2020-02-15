// Copyright 2019 CUE Authors
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
	"github.com/spf13/pflag"
)

// Common flags
const (
	flagAll      flagName = "all"
	flagDryrun   flagName = "dryrun"
	flagVerbose  flagName = "verbose"
	flagTrace    flagName = "trace"
	flagForce    flagName = "force"
	flagIgnore   flagName = "ignore"
	flagSimplify flagName = "simplify"
	flagPackage  flagName = "package"
	flagTags     flagName = "tags"

	flagExpression flagName = "expression"
	flagSchema     flagName = "schema"
	flagEscape     flagName = "escape"
	flagGlob       flagName = "name"
	flagRecursive  flagName = "recursive"
	flagType       flagName = "type"
	flagList       flagName = "list"
	flagPath       flagName = "path"
)

var flagMedia = stringFlag{
	name: "out",
	text: "output format (json, yaml or text)",
	def:  "json",
}

var flagOut = stringFlag{
	name:  "out",
	short: "o",
	text:  "alternative output or - for stdout",
}

func addGlobalFlags(f *pflag.FlagSet) {
	f.Bool(string(flagTrace), false,
		"trace computation")
	f.BoolP(string(flagSimplify), "s", false,
		"simplify output")
	f.BoolP(string(flagIgnore), "i", false,
		"proceed in the presence of errors")
	f.BoolP(string(flagVerbose), "v", false,
		"print information about progress")
}

func addOrphanFlags(f *pflag.FlagSet) {
	f.StringP(string(flagPackage), "p", "", "package name for non-CUE files")
	f.StringArrayP(string(flagPath), "l", nil, "CUE expression for single path component")
	f.Bool(string(flagList), false, "concatenate multiple objects into a list")
	f.Bool(string(flagFiles), false, "split multiple entries into different files")
	f.Bool(string(flagWithContext), false, "import as object with contextual data")
	f.StringArrayP(string(flagProtoPath), "I", nil, "paths in which to search for imports")
	f.StringP(string(flagGlob), "n", "", "glob filter for file names")
}

type flagName string

func (f flagName) Bool(cmd *Command) bool {
	v, _ := cmd.Flags().GetBool(string(f))
	return v
}

func (f flagName) String(cmd *Command) string {
	v, _ := cmd.Flags().GetString(string(f))
	return v
}

func (f flagName) StringArray(cmd *Command) []string {
	v, _ := cmd.Flags().GetStringArray(string(f))
	return v
}

type stringFlag struct {
	name  string
	short string
	text  string
	def   string
}

func (f *stringFlag) Add(cmd *cobra.Command) {
	cmd.Flags().StringP(f.name, f.short, f.def, f.text)
}

func (f *stringFlag) String(cmd *Command) string {
	v, err := cmd.Flags().GetString(f.name)
	if err != nil {
		return f.def
	}
	return v
}
