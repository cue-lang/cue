// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cmd handles the cuelsp command line.
// It contains a handler for each of the modes, along with all the flag handling
// and the command line output format.
package cmd

import (
	"context"
	"flag"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/tool"
)

// Application is the main application as passed to tool.Main
// It handles the main command line parsing and dispatch to the sub commands.
type Application struct {
	// Core application flags

	// Embed the basic profiling flags supported by the tool package
	tool.Profile

	// We include the server configuration directly for now, so the flags work
	// even without the verb.
	// TODO: Remove this when we stop allowing the serve verb by default.
	Serve Serve

	// the options configuring function to invoke when building a server
	options func(*settings.Options)

	// Support for remote LSP server.
	Remote string `flag:"remote" help:"forward all commands to a remote lsp specified by this flag. With no special prefix, this is assumed to be a TCP address. If prefixed by 'unix;', the subsequent address is assumed to be a unix domain socket. If 'auto', or prefixed by 'auto;', the remote address is automatically resolved based on the executing environment."`

	// Verbose enables verbose logging.
	Verbose bool `flag:"v,verbose" help:"verbose output"`

	// VeryVerbose enables a higher level of verbosity in logging output.
	VeryVerbose bool `flag:"vv,veryverbose" help:"very verbose output"`

	// PrepareOptions is called to update the options when a new view is built.
	// It is primarily to allow the behavior of gopls to be modified by hooks.
	PrepareOptions func(*settings.Options)
}

// New returns a new Application ready to run.
func New(options func(*settings.Options)) *Application {
	app := &Application{
		options: options,

		Serve: Serve{
			RemoteListenTimeout: 1 * time.Minute,
		},
	}
	app.Serve.app = app
	return app
}

// Name implements tool.Application returning the binary name.
func (app *Application) Name() string { return "cuelsp" }

// Usage implements tool.Application returning empty extra argument usage.
func (app *Application) Usage() string { return "" }

// ShortHelp implements tool.Application returning the main binary help.
func (app *Application) ShortHelp() string {
	return ""
}

// DetailedHelp implements tool.Application returning the main binary help.
// This includes the short help for all the sub commands.
func (app *Application) DetailedHelp(f *flag.FlagSet) {
	w := tabwriter.NewWriter(f.Output(), 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprint(w, `
cuelsp is a CUE language server.

It is typically used with an editor to provide language features. When no
command is specified, cuelsp will default to the 'serve' command. The language
features can also be accessed via the cuelsp command-line interface.

Usage:
  cuelsp help [<subject>]

Command:
`)
	fmt.Fprint(w, "\nMain\t\n")
	for _, c := range app.mainCommands() {
		fmt.Fprintf(w, "  %s\t%s\n", c.Name(), c.ShortHelp())
	}
	fmt.Fprint(w, "\t\nFeatures\t\n")
	for _, c := range app.featureCommands() {
		fmt.Fprintf(w, "  %s\t%s\n", c.Name(), c.ShortHelp())
	}
	fmt.Fprint(w, "\nflags:\n")
	printFlagDefaults(f)
}

// this is a slightly modified version of flag.PrintDefaults to give us control
func printFlagDefaults(s *flag.FlagSet) {
	var flags [][]*flag.Flag
	seen := map[flag.Value]int{}
	s.VisitAll(func(f *flag.Flag) {
		if i, ok := seen[f.Value]; !ok {
			seen[f.Value] = len(flags)
			flags = append(flags, []*flag.Flag{f})
		} else {
			flags[i] = append(flags[i], f)
		}
	})
	for _, entry := range flags {
		sort.SliceStable(entry, func(i, j int) bool {
			return len(entry[i].Name) < len(entry[j].Name)
		})
		var b strings.Builder
		for i, f := range entry {
			switch i {
			case 0:
				b.WriteString("  -")
			default:
				b.WriteString(",-")
			}
			b.WriteString(f.Name)
		}

		f := entry[0]
		name, usage := flag.UnquoteUsage(f)
		if len(name) > 0 {
			b.WriteString("=")
			b.WriteString(name)
		}
		// Boolean flags of one ASCII letter are so common we
		// treat them specially, putting their usage on the same line.
		if b.Len() <= 4 { // space, space, '-', 'x'.
			b.WriteString("\t")
		} else {
			// Four spaces before the tab triggers good alignment
			// for both 4- and 8-space tab stops.
			b.WriteString("\n    \t")
		}
		b.WriteString(strings.ReplaceAll(usage, "\n", "\n    \t"))
		if !isZeroValue(f, f.DefValue) {
			if reflect.TypeOf(f.Value).Elem().Name() == "stringValue" {
				fmt.Fprintf(&b, " (default %q)", f.DefValue)
			} else {
				fmt.Fprintf(&b, " (default %v)", f.DefValue)
			}
		}
		fmt.Fprint(s.Output(), b.String(), "\n")
	}
}

// isZeroValue is copied from the flags package
func isZeroValue(f *flag.Flag, value string) bool {
	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	typ := reflect.TypeOf(f.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Pointer {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	return value == z.Interface().(flag.Value).String()
}

// Run takes the args after top level flag processing, and invokes the correct
// sub command as specified by the first argument.
// If no arguments are passed it will invoke the server sub command, as a
// temporary measure for compatibility.
func (app *Application) Run(ctx context.Context, args ...string) error {
	if len(args) == 0 {
		s := flag.NewFlagSet(app.Name(), flag.ExitOnError)
		return tool.Run(ctx, s, &app.Serve, args)
	}
	command, args := args[0], args[1:]
	for _, c := range app.Commands() {
		if c.Name() == command {
			s := flag.NewFlagSet(app.Name(), flag.ExitOnError)
			return tool.Run(ctx, s, c, args)
		}
	}
	return tool.CommandLineErrorf("Unknown command %v", command)
}

// Commands returns the set of commands supported by the cuelsp tool on the
// command line.
// The command is specified by the first non flag argument.
func (app *Application) Commands() []tool.Application {
	var commands []tool.Application
	commands = append(commands, app.mainCommands()...)
	commands = append(commands, app.featureCommands()...)
	return commands
}

func (app *Application) mainCommands() []tool.Application {
	return []tool.Application{
		&app.Serve,
		&help{app: app},
		&apiJSON{app: app},
		&licenses{app: app},
	}
}

func (app *Application) featureCommands() []tool.Application {
	return nil
}
