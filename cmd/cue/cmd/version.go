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

const (
	defaultVersion = "devel"
)

// version be set by a builder using
// -ldflags='-X cuelang.org/go/cmd/cue/cmd.version=<version>'.
// However, people should prefer building via a mechanism which
// resolves cuelang.org/go as a dependency (and not the main
// module), in which case the version information is determined
// from the *debug.BuildInfo (see below). So this mechanism is
// really considered legacy.
var (
	version = defaultVersion
)

func runVersion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()
	if bi, ok := debug.ReadBuildInfo(); ok && version == defaultVersion {
		// No specific version provided via version
		version = bi.Main.Version
	}
	fmt.Fprintf(w, "cue version %v %s/%s\n",
		version,
		goruntime.GOOS, goruntime.GOARCH,
	)
	return nil
}
