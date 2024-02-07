// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"flag"
	"fmt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// check implements the check verb for gopls.
type check struct {
	app *Application
}

func (c *check) Name() string      { return "check" }
func (c *check) Parent() string    { return c.app.Name() }
func (c *check) Usage() string     { return "<filename>" }
func (c *check) ShortHelp() string { return "show diagnostic results for the specified file" }
func (c *check) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), `
Example: show the diagnostic results of this file:

	$ gopls check internal/cmd/check.go
`)
	printFlagDefaults(f)
}

// Run performs the check on the files specified by args and prints the
// results to stdout.
func (c *check) Run(ctx context.Context, args ...string) error {
	if len(args) == 0 {
		// no files, so no results
		return nil
	}
	checking := map[protocol.DocumentURI]*cmdFile{}
	var uris []protocol.DocumentURI
	// now we ready to kick things off
	conn, err := c.app.connect(ctx, nil)
	if err != nil {
		return err
	}
	defer conn.terminate(ctx)
	for _, arg := range args {
		uri := protocol.URIFromPath(arg)
		uris = append(uris, uri)
		file, err := conn.openFile(ctx, uri)
		if err != nil {
			return err
		}
		checking[uri] = file
	}
	if err := conn.diagnoseFiles(ctx, uris); err != nil {
		return err
	}
	conn.client.filesMu.Lock()
	defer conn.client.filesMu.Unlock()

	for _, file := range checking {
		for _, d := range file.diagnostics {
			spn, err := file.rangeSpan(d.Range)
			if err != nil {
				return fmt.Errorf("Could not convert position %v for %q", d.Range, d.Message)
			}
			fmt.Printf("%v: %v\n", spn, d.Message)
		}
	}
	return nil
}
