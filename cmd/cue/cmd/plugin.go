package cmd

import (
	"github.com/spf13/cobra"
)

func newPluginCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		Use:   "plugin <cmd> [arguments]",
		Short: "manage Go plugins",
		Long: `Plugin groups commands which manage Go plugins for CUE modules.

Go plugins allow CUE code to call Go functions via @extern(go) attributes.
The "cue plugin build" command generates and builds a plugin binary that
extends the cue command with Go function implementations.
`,
	})
	cmd.AddCommand(newPluginBuildCmd(c))
	cmd.AddCommand(newPluginGenerateGoCmd(c))
	return cmd
}
