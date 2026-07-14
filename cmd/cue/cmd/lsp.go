// Copyright 2024 The CUE Authors
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
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	"cuelang.org/go/internal/golangorgx/gopls/lsprpc"
	"cuelang.org/go/internal/golangorgx/tools/fakenet"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/unstable/lspaux/validatorconfig"
)

func newLSPCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "start or interact with a CUE Language Server instance",
		Long: `
lsp runs a CUE language server.

The server communicates using the Language Server Protocol (LSP), with
JSONRPC2 over stdin and stdout, and is intended to be run as a child
process of an editor. Running "cue lsp" with no subcommand is
equivalent to running "cue lsp serve".

By default, the server serves a single editor session over stdin and
stdout. It can instead run as a shared daemon serving several editors:
start the daemon with --listen, and configure each editor to run
"cue lsp --remote" to connect to it. With --remote=auto, an editor's
"cue lsp" starts the daemon automatically if it is not already
running.
`[1:],
		RunE: mkRunE(c, runLSPServe),
	}

	pf := cmd.PersistentFlags()
	pf.String(string(flagListen), "",
		"run as a daemon, listening on this address. If prefixed by 'unix;', the subsequent address is assumed to be a unix domain socket. Otherwise, TCP is used.")
	pf.Duration(string(flagListenTimeout), 0,
		"when used with --listen, shut down the server when there are no connected clients for this duration")
	pf.String(string(flagRemote), "",
		"forward the connection to a remote server at this address. With no special prefix, this is assumed to be a TCP address. If prefixed by 'unix;', the subsequent address is assumed to be a unix domain socket. If 'auto', or prefixed by 'auto;', the remote address is automatically resolved based on the executing environment.")
	pf.Duration(string(flagRemoteListenTimeout), 1*time.Minute,
		"when used with --remote=auto, the --listen-timeout value used to start the daemon")
	pf.String(string(flagExtConfig), "",
		"path to config file for external validators")
	pf.String(string(flagExtProfile), "",
		"profile name for external validators")

	cmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "run a CUE language server",
		Long: `
serve runs a CUE language server. This is the default command of
"cue lsp"; see "cue help lsp" for details.
`[1:],
		RunE: mkRunE(c, runLSPServe),
	})

	return cmd
}

func runLSPServe(cmd *Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}

	listen := flagListen.String(cmd)
	// A server started via --listen serves several editor sessions
	// over time, and so must outlive any single one of them.
	isDaemon := listen != ""

	var ss jsonrpc2.StreamServer
	if remote := flagRemote.String(cmd); remote != "" {
		// remoteArgs returns the command line with which "cue lsp
		// --remote=auto" starts the daemon it will then connect to.
		remoteListenTimeout := flagRemoteListenTimeout.Duration(cmd)
		remoteArgs := func(network, address string) []string {
			args := []string{"lsp", "serve",
				"--listen", fmt.Sprintf("%s;%s", network, address),
			}
			if remoteListenTimeout != 0 {
				args = append(args, "--listen-timeout", remoteListenTimeout.String())
			}
			return args
		}
		var err error
		ss, err = lsprpc.NewForwarder(remote, remoteArgs)
		if err != nil {
			return fmt.Errorf("creating forwarder: %w", err)
		}
	} else {
		profile, err := externalValidatorProfile(cmd)
		if err != nil {
			return err
		}
		cache, err := cache.New(profile)
		if err != nil {
			return err
		}
		ss = lsprpc.NewStreamServer(cache, isDaemon, hooks.Options)
	}

	ctx := cmd.Context()
	if isDaemon {
		network, addr := lsprpc.ParseAddr(listen)
		log.Printf("cuelsp daemon: listening on %s network, address %s...", network, addr)
		defer log.Printf("cuelsp daemon: exiting")
		err := jsonrpc2.ListenAndServe(ctx, network, addr, ss, flagListenTimeout.Duration(cmd))
		if errors.Is(err, jsonrpc2.ErrIdleTimeout) {
			// Expiry of the idle timeout is the intended way for a
			// daemon to shut down, not a failure.
			return nil
		}
		return err
	}
	stream := jsonrpc2.NewHeaderStream(fakenet.NewConn("stdio", os.Stdin, os.Stdout))
	conn := jsonrpc2.NewConn(stream)
	err := ss.ServeStream(ctx, conn)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// externalValidatorProfile returns the external validator profile
// selected by the --extconfig and --extprofile flags, or nil if
// --extconfig is unset.
func externalValidatorProfile(cmd *Command) (*validatorconfig.Profile, error) {
	configFile := flagExtConfig.String(cmd)
	profileName := flagExtProfile.String(cmd)
	if configFile == "" {
		if profileName != "" {
			return nil, errors.New("--extprofile can only be set in conjunction with --extconfig")
		}
		return nil, nil
	}
	cfg, err := validatorconfig.Parse(configFile)
	if err != nil {
		return nil, fmt.Errorf("reading external config file: %w", err)
	}
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	profile, found := cfg.Profiles[profileName]
	if !found {
		return nil, fmt.Errorf("profile %q not found in config file %s", profileName, configFile)
	}

	profile.ServerURL = strings.TrimRight(profile.ServerURL, "/")
	return profile, nil
}
