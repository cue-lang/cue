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
	"errors"
	"fmt"
	logger "log"
	"os"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

var cfgFile string

// newRootCmd creates the base command when called without any subcommands
func newRootCmd() *cobra.Command {
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

	subCommands := []*cobra.Command{
		newTrimCmd(),
		newImportCmd(),
		newEvalCmd(),
		newGetCmd(),
		newFmtCmd(),
		newExportCmd(),
		newCmdCmd(),
		newVetCmd(),
		newAddCmd(),
	}

	addGlobalFlags(cmd.PersistentFlags())

	for _, sub := range subCommands {
		cmd.AddCommand(sub)
	}

	return cmd
}

// Main runs the cue tool. It loads the tool flags.
func Main(ctx context.Context, args []string) (err error) {
	log.SetFlags(0)
	// Three categories of commands:
	// - normal
	// - user defined
	// - help
	// For the latter two, we need to use the default loading.
	defer func() {
		switch e := recover().(type) {
		case nil:
		case panicError:
			err = e.Err
		default:
			panic(err)
		}
		// We use panic to escape, instead of os.Exit
	}()
	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	if len(args) >= 1 && args[0] != "help" {
		// TODO: for now we only allow one instance. Eventually, we can allow
		// more if they all belong to the same package and we merge them
		// before computing commands.
		if cmd, _, err := rootCmd.Find(args); err != nil || cmd == nil {
			tools := buildTools(rootCmd, args[1:])
			addCustom(rootCmd, commandSection, args[0], tools)
		}

		type subSpec struct {
			name string
			cmd  *cobra.Command
		}
		sub := map[string]subSpec{
			"cmd": {commandSection, newCmdCmd()},
			// "serve": {"server", nil},
			// "fix":   {"fix", nil},
		}
		if sub, ok := sub[args[0]]; ok && len(args) >= 2 {
			args = args[1:]
			if len(args) == 0 {
				tools := buildTools(rootCmd, args)
				// list available commands
				commands := tools.Lookup(sub.name)
				i, err := commands.Fields()
				if err != nil {
					return err
				}
				for i.Next() {
					_, _ = addCustom(sub.cmd, sub.name, i.Label(), tools)
				}
				return nil
			}
			tools := buildTools(rootCmd, args[1:])
			_, err := addCustom(sub.cmd, sub.name, args[0], tools)
			if err != nil {
				log.Printf("%s %q is not defined", sub.name, args[0])
				exit()
			}
		}
	}
	return rootCmd.Execute()
}

type panicError struct {
	Err error
}

func exit() {
	panic(panicError{errors.New("terminating because of errors")})
}

func init() {
	cobra.OnInitialize(initConfig)

}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".cue" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".cue")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
