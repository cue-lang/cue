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
	"testing"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/cueversion"
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

// version can be set at build time to inject cmd/cue's version string,
// particularly necessary when building a release locally.
// See the top-level README.md for more details.
var version string

func runVersion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()

	// read in build info
	bi, ok := readBuildInfo()
	if !ok {
		// shouldn't happen
		return errors.New("unknown error reading build-info")
	}
	fmt.Fprintf(w, "cue version %s\n\n", cueModuleVersion())
	fmt.Fprintf(w, "go version %s\n", runtime.Version())
	bi.Settings = append(bi.Settings, debug.BuildSetting{
		Key:   "cue.lang.version",
		Value: cueversion.LanguageVersion(),
	})
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
		// Empirically, 16 is enough; the longest key seen outside our own "cue.lang.version"
		// is "vcs.revision".
		fmt.Fprintf(w, "%16s %s\n", s.Key, s.Value)
	}
	return nil
}

// cueModuleVersion returns the version of the cuelang.org/go module as much
// as can reasonably be determined. If no version can be determined,
// it returns the empty string.
func cueModuleVersion() string {
	if v := version; v != "" {
		// The global version variable has been configured via ldflags.
		return v
	}
	return cueversion.ModuleVersion()
}

func readBuildInfo() (*debug.BuildInfo, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok || !testing.Testing() {
		return bi, ok
	}
	// test-based overrides
	if v := os.Getenv("CUE_VERSION_TEST_CFG"); v != "" {
		var extra []debug.BuildSetting
		if err := json.Unmarshal([]byte(v), &extra); err != nil {
			// It's only for tests, so panic is OK.
			panic(err)
		}
		bi.Settings = append(bi.Settings, extra...)
	}
	return bi, true
}
