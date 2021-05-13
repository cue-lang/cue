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
	flagAll        flagName = "all"
	flagDryrun     flagName = "dryrun"
	flagVerbose    flagName = "verbose"
	flagAllErrors  flagName = "all-errors"
	flagTrace      flagName = "trace"
	flagForce      flagName = "force"
	flagIgnore     flagName = "ignore"
	flagStrict     flagName = "strict"
	flagSimplify   flagName = "simplify"
	flagPackage    flagName = "package"
	flagInject     flagName = "inject"
	flagInjectVars flagName = "inject-vars"

	flagExpression  flagName = "expression"
	flagSchema      flagName = "schema"
	flagEscape      flagName = "escape"
	flagGlob        flagName = "name"
	flagRecursive   flagName = "recursive"
	flagMerge       flagName = "merge"
	flagList        flagName = "list"
	flagPath        flagName = "path"
	flagFiles       flagName = "files"
	flagProtoPath   flagName = "proto_path"
	flagProtoEnum   flagName = "proto_enum"
	flagExt         flagName = "ext"
	flagWithContext flagName = "with-context"
	flagOut         flagName = "out"
	flagOutFile     flagName = "outfile"
)

func addOutFlags(f *pflag.FlagSet, allowNonCUE bool) {
	if allowNonCUE {
		f.String(string(flagOut), "",
			`output format (run 'cue filetypes' for more info)`)
	}
	f.StringP(string(flagOutFile), "o", "",
		`filename or - for stdout with optional file prefix (run 'cue filetypes' for more info)`)
	f.BoolP(string(flagForce), "f", false, "force overwriting existing files")
}

func addGlobalFlags(f *pflag.FlagSet) {
	f.Bool(string(flagTrace), false,
		"trace computation")
	f.BoolP(string(flagSimplify), "s", false,
		"simplify output")
	f.BoolP(string(flagIgnore), "i", false,
		"proceed in the presence of errors")
	f.Bool(string(flagStrict), false,
		"report errors for lossy mappings")
	f.BoolP(string(flagVerbose), "v", false,
		"print information about progress")
	f.BoolP(string(flagAllErrors), "E", false, "print all available errors")
}

func addOrphanFlags(f *pflag.FlagSet) {
	f.StringP(string(flagPackage), "p", "", "package name for non-CUE files")
	f.StringP(string(flagSchema), "d", "",
		"expression to select schema for evaluating values in non-CUE files")
	f.StringArrayP(string(flagPath), "l", nil, "CUE expression for single path component")
	f.Bool(string(flagList), false, "concatenate multiple objects into a list")
	f.Bool(string(flagWithContext), false, "import as object with contextual data")
	f.StringArrayP(string(flagProtoPath), "I", nil, "paths in which to search for imports")
	f.String(string(flagProtoEnum), "int", "mode for rendering enums (int|json)")
	f.StringP(string(flagGlob), "n", "", "glob filter for non-CUE file names in directories")
	f.Bool(string(flagMerge), true, "merge non-CUE files")
}

func addInjectionFlags(f *pflag.FlagSet, auto bool) {
	f.StringArrayP(string(flagInject), "t", nil,
		"set the value of a tagged field")
	f.BoolP(string(flagInjectVars), "T", auto,
		"inject system variables in tags")
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
