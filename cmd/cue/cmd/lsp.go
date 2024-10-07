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

	goplscmd "cuelang.org/go/internal/golangorgx/gopls/cmd"
	"cuelang.org/go/internal/golangorgx/gopls/hooks"
	"cuelang.org/go/internal/golangorgx/tools/tool"
	"github.com/spf13/cobra"
)

func newLSPCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "lsp",
		Short:  "interact with a CUE Language Server instance",
		Run: func(cmd *cobra.Command, args []string) {
			c.Command = cmd
			runLSP(c, args)
		},
		DisableFlagParsing: true,
	}

	// TODO(myitcv): flesh out docs.

	// TODO(myitcv): move the LSP towards the same flag processing as used here
	// in cmd/cue.

	// TODO(myitcv): add some means for 'cue help lsp' triggering 'cue lsp
	// -help' until such time as we flip the LSP command itself over to cobra
	// (if that's what we want to do).

	// TODO(myitcv): prevent the 'lsp' command from inheriting the root flags.

	return cmd
}

func runLSP(cmd *Command, args []string) {
	ctx := context.Background()
	tool.Main(ctx, goplscmd.New(hooks.Options), args)
}
