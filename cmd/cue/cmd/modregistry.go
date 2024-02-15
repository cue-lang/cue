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
	"fmt"
	"net"
	"net/http"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"github.com/spf13/cobra"
)

// TODO: add testing for this command.

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

Note: this command might be removed or changed significantly in the future.
`,
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
	return http.Serve(l, ociserver.New(ociTagLoggerRegistry{r}, nil))
}

type ociTagLoggerRegistry struct {
	ociregistry.Interface
}

func (r ociTagLoggerRegistry) PushManifest(ctx context.Context, repo string, tag string, contents []byte, mediaType string) (ociregistry.Descriptor, error) {
	fmt.Printf("tagged %v@%v\n", repo, tag)
	return r.Interface.PushManifest(ctx, repo, tag, contents, mediaType)
}
