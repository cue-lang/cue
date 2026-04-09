// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/lsprpc"
	"cuelang.org/go/internal/golangorgx/tools/fakenet"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/golangorgx/tools/tool"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/unstable/lspaux/validatorconfig"
)

// Serve is a struct that exposes the configurable parts of the LSP server as
// flags, in the right form for tool.Main to consume.
type Serve struct {
	ExtConfigFile string `flag:"extconfig" help:"path to config file for external validators"`
	ExtProfile    string `flag:"extprofile" help:"profile name for external validators"`

	app *Application
}

func (s *Serve) Name() string   { return "serve" }
func (s *Serve) Parent() string { return s.app.Name() }
func (s *Serve) Usage() string  { return "[server-flags]" }
func (s *Serve) ShortHelp() string {
	return "run a server for CUE code using the Language Server Protocol"
}
func (s *Serve) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), `  cuelsp [flags] [server-flags]

The server communicates using JSONRPC2 on stdin and stdout, and is intended to be run directly as
a child of an editor process.

server-flags:
`)
	printFlagDefaults(f)
}

// Run configures a server based on the flags, and then runs it.
// It blocks until the server shuts down.
func (s *Serve) Run(ctx context.Context, args ...string) error {
	if len(args) > 0 {
		return tool.CommandLineErrorf("server does not take arguments, got %v", args)
	}

	profile, err := s.externalValidatorProfile()
	if err != nil {
		return err
	}
	c, err := cache.New(profile)
	if err != nil {
		return err
	}
	ss := lsprpc.NewStreamServer(c, false, s.app.options)

	stream := jsonrpc2.NewHeaderStream(fakenet.NewConn("stdio", os.Stdin, os.Stdout))
	conn := jsonrpc2.NewConn(stream)
	err = ss.ServeStream(ctx, conn)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *Serve) externalValidatorProfile() (*validatorconfig.Profile, error) {
	if s.ExtConfigFile == "" {
		if s.ExtProfile != "" {
			return nil, fmt.Errorf("-extprofile can only be set in conjunction with -extconfig")
		}
		return nil, nil
	}
	cfg, err := validatorconfig.Parse(s.ExtConfigFile)
	if err != nil {
		return nil, fmt.Errorf("reading external config file: %w", err)
	}
	profileName := cfg.ActiveProfile
	if s.ExtProfile != "" {
		profileName = s.ExtProfile
	}
	profile, found := cfg.Profiles[profileName]
	if !found {
		return nil, fmt.Errorf("profile %q not found in config file %s", profileName, s.ExtConfigFile)
	}

	profile.ServerURL = strings.TrimRight(profile.ServerURL, "/")
	return profile, nil
}
