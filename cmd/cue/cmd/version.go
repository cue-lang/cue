// Copyright 2019 CUE Authors
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
	"fmt"
	goruntime "runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func newVersionCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "print CUE version",
		Long:  ``,
		RunE:  mkRunE(c, runVersion),
	}
	return cmd
}

func runVersion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()
	var version = "devel"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Sum != "" {
		version = fmt.Sprintf("%s (%s)", bi.Main.Version, bi.Main.Sum)
	}
	fmt.Fprintf(w, "cue version %v %s/%s\n",
		version,
		goruntime.GOOS, goruntime.GOARCH,
	)
	return nil
}
