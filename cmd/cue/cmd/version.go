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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
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

const defaultVersion = "(devel)"

// version be set by a builder using
// -ldflags='-X cuelang.org/go/cmd/cue/cmd.version=<version>'.
// However, people should prefer building via a mechanism which
// resolves cuelang.org/go as a dependency (and not the main
// module), in which case the version information is determined
// from the *debug.BuildInfo (see below). So this mechanism is
// really considered legacy.
var version = defaultVersion

func runVersion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()
	// read in build info
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		// shouldn't happen
		return errors.New("unknown error reading build-info")
	}

	// test-based overrides
	if v := os.Getenv("CUE_VERSION_TEST_CFG"); v != "" {
		var extra []debug.BuildSetting
		if err := json.Unmarshal([]byte(v), &extra); err != nil {
			return err
		}
		bi.Settings = append(bi.Settings, extra...)
	}

	// prefer ldflags `version` override
	if version == defaultVersion {
		// no version provided via ldflags, try buildinfo
		if bi.Main.Version != "" && bi.Main.Version != defaultVersion {
			version = bi.Main.Version
		}
	}

	if version == defaultVersion {
		// a specific version was not provided by ldflags or buildInfo
		// attempt to make our own
		var vcsTime time.Time
		var vcsRevision string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.time":
				// If the format is invalid, we'll print a zero timestamp.
				vcsTime, _ = time.Parse(time.RFC3339Nano, s.Value)
			case "vcs.revision":
				vcsRevision = s.Value
				// module.PseudoVersion recommends the revision to be a 12-byte
				// commit hash prefix, which is what cmd/go uses as well.
				if len(vcsRevision) > 12 {
					vcsRevision = vcsRevision[:12]
				}
			}
		}
		if vcsRevision != "" {
			version = module.PseudoVersion("", "", vcsTime, vcsRevision)
		}
	}

	fmt.Fprintf(w, "cue version %s\n\n", version)
	fmt.Fprintf(w, "go version %s\n", runtime.Version())
	for _, s := range bi.Settings {
		if s.Value == "" {
			// skip empty build settings
			continue
		}
		// The padding helps keep readability by aligning:
		//
		//   veryverylong.key value
		//          short.key some-other-value
		//
		// Empirically, 16 is enough; the longest key seen is "vcs.revision".
		fmt.Fprintf(w, "%16s %s\n", s.Key, s.Value)
	}
	return nil
}
