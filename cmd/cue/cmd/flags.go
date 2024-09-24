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
	"fmt"

	"github.com/spf13/pflag"
)

// Common flags
const (
	flagAll             flagName = "all"
	flagAllErrors       flagName = "all-errors"
	flagCheck           flagName = "check"
	flagDiff            flagName = "diff"
	flagDryRun          flagName = "dry-run"
	flagEscape          flagName = "escape"
	flagExpression      flagName = "expression"
	flagExt             flagName = "ext"
	flagFiles           flagName = "files"
	flagForce           flagName = "force"
	flagGlob            flagName = "name"
	flagIgnore          flagName = "ignore"
	flagInject          flagName = "inject"
	flagInjectVars      flagName = "inject-vars"
	flagInlineImports   flagName = "inline-imports"
	flagJSON            flagName = "json"
	flagLanguageVersion flagName = "language-version"
	flagList            flagName = "list"
	flagMerge           flagName = "merge"
	flagOut             flagName = "out"
	flagOutFile         flagName = "outfile"
	flagPackage         flagName = "package"
	flagPath            flagName = "path"
	flagProtoEnum       flagName = "proto_enum"
	flagProtoPath       flagName = "proto_path"
	flagRecursive       flagName = "recursive"
	flagSchema          flagName = "schema"
	flagSimplify        flagName = "simplify"
	flagSource          flagName = "source"
	flagStrict          flagName = "strict"
	flagTrace           flagName = "trace"
	flagVerbose         flagName = "verbose"
	flagWithContext     flagName = "with-context"

	// Hidden flags.
	flagCpuProfile flagName = "cpuprofile"
	flagMemProfile flagName = "memprofile"
)

func addOutFlags(f *pflag.FlagSet, allowNonCUE bool) {
	if allowNonCUE {
		f.String(string(flagOut), "",
			`output format (run 'cue help filetypes' for more info)`)
	}
	f.StringP(string(flagOutFile), "o", "",
		`filename or - for stdout with optional file prefix (run 'cue help filetypes' for more info)`)
	f.BoolP(string(flagForce), "f", false, "force overwriting existing files")
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
	f.BoolP(string(flagAllErrors), "E", false, "print all available errors")

	// Deprecated flags are hidden but still work for now.
	// TODO(mvdan): make this flag give a warning or error in early 2025.
	f.Bool(string(flagStrict), false,
		"report errors for lossy mappings (deprecated: use \"jsonschema+strict:\" filetype instead; see cue help filetypes)")
	f.MarkHidden(string(flagStrict))

	f.String(string(flagCpuProfile), "", "write a CPU profile to the specified file before exiting")
	f.MarkHidden(string(flagCpuProfile))
	f.String(string(flagMemProfile), "", "write an allocation profile to the specified file before exiting")
	f.MarkHidden(string(flagMemProfile))
}

func addOrphanFlags(f *pflag.FlagSet) {
	f.StringP(string(flagPackage), "p", "", "package name for non-CUE files")
	f.StringP(string(flagSchema), "d", "",
		"expression to select schema for evaluating values in non-CUE files")
	f.StringArrayP(string(flagPath), "l", nil, "CUE expression for single path component (see 'cue help flags' for details)")
	f.Bool(string(flagList), false, "concatenate multiple objects into a list")
	f.Bool(string(flagWithContext), false, "import as object with contextual data")
	f.StringArrayP(string(flagProtoPath), "I", nil, "paths in which to search for imports")
	f.String(string(flagProtoEnum), "int", "mode for rendering enums (int|json)")
	f.StringP(string(flagGlob), "n", "", "glob filter for non-CUE file names in directories")
	f.Bool(string(flagMerge), true, "merge non-CUE files")
}

func addInjectionFlags(f *pflag.FlagSet, auto, hidden bool) {
	f.StringArrayP(string(flagInject), "t", nil,
		"set the value of a tagged field")
	f.BoolP(string(flagInjectVars), "T", auto,
		"inject system variables in tags")
	if hidden {
		f.Lookup(string(flagInject)).Hidden = true
		f.Lookup(string(flagInjectVars)).Hidden = true
	}
}

type flagName string

// ensureAdded detects if a flag is being used without it first being
// added to the flagSet. Because flagNames are global, it is quite
// easy to accidentally use a flag in a command without adding it to
// the flagSet.
func (f flagName) ensureAdded(cmd *Command) {
	if cmd.Flags().Lookup(string(f)) == nil {
		panic(fmt.Sprintf("Cmd %q uses flag %q without adding it", cmd.Name(), f))
	}
}

func (f flagName) Bool(cmd *Command) bool {
	f.ensureAdded(cmd)
	v, _ := cmd.Flags().GetBool(string(f))
	return v
}

func (f flagName) String(cmd *Command) string {
	f.ensureAdded(cmd)
	v, _ := cmd.Flags().GetString(string(f))
	return v
}

func (f flagName) StringArray(cmd *Command) []string {
	f.ensureAdded(cmd)
	v, _ := cmd.Flags().GetStringArray(string(f))
	return v
}
