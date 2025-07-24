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
	"log"
	"os"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/lsprpc"
	"cuelang.org/go/internal/golangorgx/tools/fakenet"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/golangorgx/tools/tool"
	"cuelang.org/go/internal/lsp/cache"
)

// Serve is a struct that exposes the configurable parts of the LSP server as
// flags, in the right form for tool.Main to consume.
type Serve struct {
	Mode        string        `flag:"mode" help:"no effect"`
	Port        int           `flag:"port" help:"port on which to run cuelsp for debugging purposes"`
	Address     string        `flag:"listen" help:"address on which to listen for remote connections. If prefixed by 'unix;', the subsequent address is assumed to be a unix domain socket. Otherwise, TCP is used."`
	IdleTimeout time.Duration `flag:"listen.timeout" help:"when used with -listen, shut down the server when there are no connected clients for this duration"`

	RemoteListenTimeout time.Duration `flag:"remote.listen.timeout" help:"when used with -remote=auto, the -listen.timeout value used to start the daemon"`

	app *Application
}

func (s *Serve) Name() string   { return "serve" }
func (s *Serve) Parent() string { return s.app.Name() }
func (s *Serve) Usage() string  { return "[server-flags]" }
func (s *Serve) ShortHelp() string {
	return "run a server for Go code using the Language Server Protocol"
}
func (s *Serve) DetailedHelp(f *flag.FlagSet) {
	fmt.Fprint(f.Output(), `  cuelsp [flags] [server-flags]

The server communicates using JSONRPC2 on stdin and stdout, and is intended to be run directly as
a child of an editor process.

server-flags:
`)
	printFlagDefaults(f)
}

func (s *Serve) remoteArgs(network, address string) []string {
	args := []string{"serve",
		"-listen", fmt.Sprintf(`%s;%s`, network, address),
	}
	if s.RemoteListenTimeout != 0 {
		args = append(args, "-listen.timeout", s.RemoteListenTimeout.String())
	}
	return args
}

// Run configures a server based on the flags, and then runs it.
// It blocks until the server shuts down.
func (s *Serve) Run(ctx context.Context, args ...string) error {
	if len(args) > 0 {
		return tool.CommandLineErrorf("server does not take arguments, got %v", args)
	}

	isDaemon := s.Address != "" || s.Port != 0
	var ss jsonrpc2.StreamServer
	if s.app.Remote != "" {
		var err error
		ss, err = lsprpc.NewForwarder(s.app.Remote, s.remoteArgs)
		if err != nil {
			return fmt.Errorf("creating forwarder: %w", err)
		}
	} else {
		cache, err := cache.New()
		if err != nil {
			return err
		}
		ss = lsprpc.NewStreamServer(cache, isDaemon, s.app.options)
	}

	var network, addr string
	if s.Address != "" {
		network, addr = lsprpc.ParseAddr(s.Address)
	}
	if s.Port != 0 {
		network = "tcp"
		// TODO(adonovan): should cuelsp ever be listening on network
		// sockets, or only local ones?
		//
		// Ian says this was added in anticipation of
		// something related to "VS Code remote" that turned
		// out to be unnecessary. So I propose we limit it to
		// localhost, if only so that we avoid the macOS
		// firewall prompt.
		//
		// Hana says: "s.Address is for the remote access (LSP)
		// and s.Port is for debugging purpose (according to
		// the Server type documentation). I am not sure why the
		// existing code here is mixing up and overwriting addr.
		// For debugging endpoint, I think localhost makes perfect sense."
		//
		// TODO(adonovan): disentangle Address and Port,
		// and use only localhost for the latter.
		addr = fmt.Sprintf(":%v", s.Port)
	}
	if addr != "" {
		log.Printf("cuelsp daemon: listening on %s network, address %s...", network, addr)
		defer log.Printf("cuelsp daemon: exiting")
		return jsonrpc2.ListenAndServe(ctx, network, addr, ss, s.IdleTimeout)
	}
	stream := jsonrpc2.NewHeaderStream(fakenet.NewConn("stdio", os.Stdin, os.Stdout))
	conn := jsonrpc2.NewConn(stream)
	err := ss.ServeStream(ctx, conn)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
