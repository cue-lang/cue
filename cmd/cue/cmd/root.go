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
	"context"
	"fmt"
	"io"
	logger "log"
	"os"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"github.com/spf13/cobra"
)

// TODO: commands
//   fix:      rewrite/refactor configuration files
//             -i interactive: open diff and ask to update
//   serve:    like cmd, but for servers
//   get:      convert cue from other languages, like proto and go.
//   gen:      generate files for other languages
//   generate  like go generate (also convert cue to go doc)
//   test      load and fully evaluate test files.
//
// TODO: documentation of concepts
//   tasks     the key element for cmd, serve, and fix

var log = logger.New(os.Stderr, "", logger.Lshortfile)

type runFunction func(cmd *Command, args []string) error

func mkRunE(c *Command, f runFunction) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		c.Command = cmd
		return f(c, args)
	}
}

// newRootCmd creates the base command when called without any subcommands
func newRootCmd() *Command {
	cmd := &cobra.Command{
		Use:   "cue",
		Short: "cue emits configuration files to user-defined commands.",
		Long: `cue evaluates CUE files, an extension of JSON, and sends them
to user-defined commands for processing.

Commands are defined in CUE as follows:

	command deploy: {
		cmd:   "kubectl"
		args:  [ "-f", "deploy" ]
		in:    json.Encode($) // encode the emitted configuration.
	}

cue can also combine the results of http or grpc request with the input
configuration for further processing. For more information on defining commands
run 'cue help cmd' or go to cuelang.org/pkg/cmd.

For more information on writing CUE configuration files see cuelang.org.`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		//	Run: func(cmd *cobra.Command, args []string) { },

		SilenceUsage: true,
	}

	c := &Command{Command: cmd, root: cmd}

	cmdCmd := newCmdCmd(c)
	c.cmd = cmdCmd

	subCommands := []*cobra.Command{
		newTrimCmd(c),
		newImportCmd(c),
		newEvalCmd(c),
		newGetCmd(c),
		newFmtCmd(c),
		newExportCmd(c),
		cmdCmd,
		newVersionCmd(c),
		newVetCmd(c),
		newAddCmd(c),
	}

	addGlobalFlags(cmd.PersistentFlags())

	for _, sub := range subCommands {
		cmd.AddCommand(sub)
	}

	return c
}

// Main runs the cue tool. It loads the tool flags.
func Main(ctx context.Context, args []string) (err error) {
	cmd, err := New(args)
	if err != nil {
		return err
	}
	err = cmd.Run(ctx)
	// TODO: remove this ugly hack. Either fix Cobra or use something else.
	stdin = nil
	return err
}

type Command struct {
	// The currently active command.
	*cobra.Command

	root *cobra.Command

	// Subcommands
	cmd *cobra.Command
}

func (c *Command) SetOutput(w io.Writer) {
	c.root.SetOutput(w)
}

func (c *Command) SetInput(r io.Reader) {
	// TODO: ugly hack. Cobra does not have a way to pass the stdin.
	stdin = r
}

func (c *Command) Run(ctx context.Context) (err error) {
	log.SetFlags(0)
	// Three categories of commands:
	// - normal
	// - user defined
	// - help
	// For the latter two, we need to use the default loading.
	defer recoverError(&err)

	return c.root.Execute()
}

func recoverError(err *error) {
	switch e := recover().(type) {
	case nil:
	case panicError:
		*err = e.Err
	default:
		panic(e)
	}
	// We use panic to escape, instead of os.Exit
}

func New(args []string) (cmd *Command, err error) {
	defer recoverError(&err)

	cmd = newRootCmd()
	rootCmd := cmd.root
	rootCmd.SetArgs(args)
	if len(args) == 0 {
		return cmd, nil
	}

	var sub = map[string]*subSpec{
		"cmd": {commandSection, cmd.cmd},
		// "serve": {"server", nil},
		// "fix":   {"fix", nil},
	}

	if args[0] == "help" {
		// Allow errors.
		_ = addSubcommands(cmd, sub, args[1:])
		return cmd, nil
	}

	if _, ok := sub[args[0]]; ok {
		return cmd, addSubcommands(cmd, sub, args)
	}

	if c, _, err := rootCmd.Find(args); err == nil && c != nil {
		return cmd, nil
	}

	if !isCommandName(args[0]) {
		return cmd, nil // Forces unknown command message from Cobra.
	}

	tools, err := buildTools(cmd, args[1:])
	if err != nil {
		return cmd, err
	}
	_, err = addCustom(cmd, rootCmd, commandSection, args[0], tools)
	if err != nil {
		fmt.Printf("command %s %q is not defined\n", commandSection, args[0])
		fmt.Println("Run 'cue help' to show available commands.")
		os.Exit(1)
	}
	return cmd, nil
}

type subSpec struct {
	name string
	cmd  *cobra.Command
}

func addSubcommands(cmd *Command, sub map[string]*subSpec, args []string) error {
	if len(args) == 0 {
		return nil
	}

	if _, ok := sub[args[0]]; ok {
		args = args[1:]
	}

	if len(args) > 0 {
		if !isCommandName(args[0]) {
			return nil // Forces unknown command message from Cobra.
		}
		args = args[1:]
	}

	tools, err := buildTools(cmd, args)
	if err != nil {
		return err
	}

	// TODO: for now we only allow one instance. Eventually, we can allow
	// more if they all belong to the same package and we merge them
	// before computing commands.
	for _, spec := range sub {
		commands := tools.Lookup(spec.name)
		if !commands.Exists() {
			return nil
		}
		i, err := commands.Fields()
		if err != nil {
			return errors.Newf(token.NoPos, "could not create command definitions: %v", err)
		}
		for i.Next() {
			_, _ = addCustom(cmd, spec.cmd, spec.name, i.Label(), tools)
		}
	}
	return nil
}

func isCommandName(s string) bool {
	return !strings.Contains(s, `/\`) &&
		!strings.HasPrefix(s, ".") &&
		!strings.HasSuffix(s, ".cue")
}

type panicError struct {
	Err error
}

func exit() {
	panic(panicError{errors.New("terminating because of errors")})
}
