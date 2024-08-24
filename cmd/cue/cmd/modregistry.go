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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"github.com/spf13/cobra"
)

func newModRegistryCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// This command is hidden, for now at least,
		// because it's not clear whether it should be
		// officially supported.
		Hidden: true,

		Use:   "registry [listen-address]",
		Short: "start a local in-memory module registry",
		Long: `
This command starts an OCI-compliant server that stores all its
contents in memory. It can serve as a scratch CUE modules registry
for use in testing.

For example, start a local registry with:

	cue mod registry localhost:8080

and point CUE_REGISTRY to it to publish a module version:

	CUE_REGISTRY=localhost:8080 cue mod publish v0.0.1

Note: this command might be removed or changed significantly in the future.
`[1:],
		RunE: mkRunE(c, runModRegistry),
		Args: cobra.MaximumNArgs(1),
	}
	return cmd
}

func runModRegistry(cmd *Command, args []string) error {
	addr := "localhost:0"
	if len(args) > 0 {
		addr = args[0]
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("listening on %v\n", l.Addr())
	r := ocimem.NewWithConfig(&ocimem.Config{
		ImmutableTags: true,
	})

	srv := http.Server{
		Handler: ociserver.New(ociTagLoggerRegistry{r}, nil),
	}
	var serveErr error
	go func() {
		if err := srv.Serve(l); !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
		}
	}()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
	<-sigint

	ctx, cancal := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancal()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("HTTP server Shutdown: %v\n", err)
		return err
	}
	return serveErr
}

type ociTagLoggerRegistry struct {
	ociregistry.Interface
}

func (r ociTagLoggerRegistry) PushManifest(ctx context.Context, repo string, tag string, contents []byte, mediaType string) (ociregistry.Descriptor, error) {
	fmt.Printf("tagged %v@%v\n", repo, tag)
	return r.Interface.PushManifest(ctx, repo, tag, contents, mediaType)
}
