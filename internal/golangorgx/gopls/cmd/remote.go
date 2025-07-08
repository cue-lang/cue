// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"cuelang.org/go/internal/golangorgx/gopls/lsprpc"
	"cuelang.org/go/internal/golangorgx/gopls/protocol/command"
)

type remote struct {
	app *Application
	subcommands
}

func newRemote(app *Application) *remote {
	return &remote{
		app: app,
		subcommands: subcommands{
			&listWorkspaces{app: app},
			&startDebugging{app: app},
		},
	}
}

func (r *remote) Name() string   { return "remote" }
func (r *remote) Parent() string { return r.app.Name() }

func (r *remote) ShortHelp() string {
	return "interact with the cuelsp daemon"
}

// listWorkspaces is an inspect subcommand to list current workspaces.
type listWorkspaces struct {
	app *Application
}

func (c *listWorkspaces) Name() string   { return "workspaces" }
func (c *listWorkspaces) Parent() string { return c.app.Name() }
func (c *listWorkspaces) Usage() string  { return "" }
func (c *listWorkspaces) ShortHelp() string {
	return "print information about current cuelsp workspaces"
}

const listWorkspacesExamples = `
Examples:

1) list workspaces for the default daemon:

$ cuelsp -remote=auto remote workspaces
or just
$ cuelsp remote workspaces

2) list workspaces for a specific daemon:

$ cuelsp -remote=localhost:8082 remote workspaces
`

func (c *listWorkspaces) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), listWorkspacesExamples)
	printFlagDefaults(f)
}

func (c *listWorkspaces) Run(ctx context.Context, args ...string) error {
	remote := c.app.Remote
	if remote == "" {
		remote = "auto"
	}
	state, err := lsprpc.QueryServerState(ctx, remote)
	if err != nil {
		return err
	}
	v, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(v)
	return nil
}

type startDebugging struct {
	app *Application
}

func (c *startDebugging) Name() string  { return "debug" }
func (c *startDebugging) Usage() string { return "[host:port]" }
func (c *startDebugging) ShortHelp() string {
	return "start the debug server"
}

const startDebuggingExamples = `
Examples:

1) start a debug server for the default daemon, on an arbitrary port:

$ cuelsp -remote=auto remote debug
or just
$ cuelsp remote debug

2) start for a specific daemon, on a specific port:

$ cuelsp -remote=localhost:8082 remote debug localhost:8083
`

func (c *startDebugging) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), startDebuggingExamples)
	printFlagDefaults(f)
}

func (c *startDebugging) Run(ctx context.Context, args ...string) error {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, c.Usage())
		return errors.New("invalid usage")
	}
	remote := c.app.Remote
	if remote == "" {
		remote = "auto"
	}
	debugAddr := ""
	if len(args) > 0 {
		debugAddr = args[0]
	}
	debugArgs := command.DebuggingArgs{
		Addr: debugAddr,
	}
	var result command.DebuggingResult
	if err := lsprpc.ExecuteCommand(ctx, remote, command.StartDebugging.ID(), debugArgs, &result); err != nil {
		return err
	}
	if len(result.URLs) == 0 {
		return errors.New("no debugging URLs")
	}
	for _, url := range result.URLs {
		fmt.Printf("debugging on %s\n", url)
	}
	return nil
}
