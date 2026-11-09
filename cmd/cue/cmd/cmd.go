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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
)

// TODO: generate long description from documentation.

func newCmdCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cmd [flags] <name> [inputs]",
		Short: "run a user-defined workflow command",
		Long: `cmd executes the named workflow command for each of the named instances.

Workflow commands are defined in tool files, which are regular CUE
files within the same package with a filename ending in _tool.cue.

Run "cue help commands" for more details on authoring tasks and
workflow commands.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			if len(args) == 0 {
				w := cmd.ErrOrStderr()
				fmt.Fprintln(w, "cmd must be run as one of its subcommands")
				fmt.Fprintln(w, "Run 'cue help cmd' for known subcommands.")
				return ErrPrintedError
			}
			tools, err := buildTools(cmd, args[1:])
			if err != nil {
				return err
			}
			sub, err := customCommand(cmd, commandSection, args[0], tools)
			if err != nil {
				w := cmd.ErrOrStderr()
				fmt.Fprint(w, errors.Details(err, &errors.Config{Cwd: rootWorkingDir()}))
				fmt.Fprintln(w, `Ensure custom commands are defined in a "_tool.cue" file.`)
				fmt.Fprintln(w, "Run 'cue help cmd' to list available custom commands.")
				return ErrPrintedError
			}
			// Presumably the *cobra.Command argument should be cmd.Command,
			// as that is the one which will have the right settings applied.
			return sub.RunE(cmd.Command, args[1:])
		}),
	}

	addInjectionFlags(cmd)
	addLanguageVersionFlag(cmd)

	// Load the custom commands so that `cue cmd --help` lists them.
	// The guard ensures we only do this for `cue cmd` itself,
	// since subcommands inherit our HelpFunc via cobra's parent walk.
	cmdCmd := cmd
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == cmdCmd {
			// With `cue cmd mycmd ./mypkg --help`, load ["./mypkg"].
			var pkgArgs []string
			if posArgs := cmd.Flags().Args(); len(posArgs) > 1 {
				pkgArgs = posArgs[1:]
			}
			addHelpCommands(c, pkgArgs)
		}
		defaultHelp(cmd, args)
	})

	return cmd
}

// addHelpCommands adds the workflow commands defined in the tool files of
// the packages in args to `cue cmd`, so that its help text lists them.
// Any error is printed to stderr, in which case it returns false.
//
// If no packages were given and those loaded have no tool files,
// there are no commands to list, so they are not built.
func addHelpCommands(c *Command, args []string) bool {
	binst, err := loadTools(c, args)
	if err != nil {
		printError(c, err)
		return false
	}
	if len(args) == 0 && !hasToolFiles(binst) {
		return true
	}
	tools, err := buildLoadedTools(c, binst)
	if err != nil {
		printError(c, err)
		return false
	}
	if tools != nil {
		addCustomCommands(c, c.cmdCmd, commandSection, tools)
	}
	return true
}

// hasToolFiles reports whether any of the loaded packages has tool files.
func hasToolFiles(binst []*build.Instance) bool {
	for _, inst := range binst {
		for _, f := range inst.BuildFiles {
			if isToolFile(f.Filename) {
				return true
			}
		}
		// A directory with multiple packages fails before any of its files
		// are added to a package, so look at the directory instead,
		// skipping the files which the loader ignores.
		var multi *load.MultiplePackageError
		if !errors.As(inst.Err, &multi) {
			continue
		}
		dir := multi.Dir // relative to the module root when resolving a package argument
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(inst.Root, dir)
		}
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && isToolFile(name) && name[0] != '_' && name[0] != '.' {
				return true
			}
		}
	}
	return false
}

// isToolFile reports whether a CUE file is a tool file going by its name.
func isToolFile(name string) bool {
	return strings.HasSuffix(name, "_tool.cue")
}
