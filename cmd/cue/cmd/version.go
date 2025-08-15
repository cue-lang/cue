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
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"

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

func runVersion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("unknown error reading build-info")
	}
	fmt.Fprintf(w, "cue version %s\n\n", cueversion.ModuleVersion())
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
