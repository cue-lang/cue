// Copyright 2026 The CUE Authors
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

package cuetestscript

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociref"

	"cuelang.org/go/mod/modregistrytest"
)

// ErrUsage is returned by a command that has been invoked incorrectly. Unlike
// any other error, which reports that the command itself failed and so may be
// negated with "!" in a script, it always fails the test.
var ErrUsage = errors.New("incorrect usage")

// Cmd holds a command made available to test scripts.
type Cmd struct {
	// Usage holds the command's usage message, without the "usage: " prefix.
	Usage string

	// RequiredEnv holds the names of the environment variables that the
	// command reads. A host which runs commands out of process passes only
	// these through.
	RequiredEnv []string

	// WritesOutput reports whether the command writes to the script's
	// standard output or standard error.
	WritesOutput bool

	// Run runs the command with the given arguments, not including the
	// command name. A non-nil error reports that the command failed, which a
	// script may expect by negating the command with "!"; the exception is
	// [ErrUsage], which always fails the test.
	Run func(e CmdEnv, args []string) error
}

// Cmds returns the commands provided to test scripts, keyed by command name.
func Cmds() map[string]Cmd {
	return maps.Clone(cmds)
}

var cmds = map[string]Cmd{
	"memregistry": {
		Usage: "memregistry [-auth=username:password] <envvar-name>",
		Run:   cmdMemRegistry,
	},
	"get-manifest": {
		Usage: "get-manifest OCI-ref@tag dest-file",
		Run:   cmdGetManifest,
	},
}

// cmdMemRegistry starts an in-memory OCI registry and sets the named
// environment variable to its host. The variable name must be one that the
// environment may set; see [ResultEnv].
func cmdMemRegistry(e CmdEnv, args []string) error {
	var auth *modregistrytest.AuthConfig
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		userPass, ok := strings.CutPrefix(args[0], "-auth=")
		if !ok {
			return ErrUsage
		}
		user, pass, ok := strings.Cut(userPass, ":")
		if !ok {
			return ErrUsage
		}
		auth = &modregistrytest.AuthConfig{
			Username: user,
			Password: pass,
		}
		args = args[1:]
	}
	if len(args) != 1 {
		return ErrUsage
	}
	srv, err := modregistrytest.NewServer(ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: true}), auth)
	if err != nil {
		return fmt.Errorf("cannot start registrytest server: %v", err)
	}
	e.Defer(srv.Close)
	e.Setenv(args[0], srv.Host())
	return nil
}

// cmdGetManifest writes the manifest for a given reference within an OCI
// registry to a file in JSON format.
func cmdGetManifest(e CmdEnv, args []string) error {
	if len(args) != 2 {
		return ErrUsage
	}
	ref, err := ociref.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid OCI reference %q: %v", args[0], err)
	}
	if ref.Tag == "" {
		return fmt.Errorf("no tag in OCI reference %q", args[0])
	}
	client, err := ociclient.New(ref.Host, &ociclient.Options{
		Insecure: true,
	})
	if err != nil {
		return err
	}
	r, err := client.GetTag(context.Background(), ref.Repository, ref.Tag)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(e.MkAbs(args[1]), data, 0o666)
}
