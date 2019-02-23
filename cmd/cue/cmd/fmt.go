// Copyright 2018 The CUE Authors
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
	"io/ioutil"
	"os"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"github.com/spf13/cobra"
)

// fmtCmd represents the fmt command
var fmtCmd = &cobra.Command{
	Use:   "fmt [-s] [packages]",
	Short: "formats CUE configuration files",
	Long: `Fmt formats the given files or the files for the given packages in place
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, inst := range load.Instances(args, nil) {
			all := []string{}
			all = append(all, inst.CUEFiles...)
			all = append(all, inst.ToolCUEFiles...)
			all = append(all, inst.TestCUEFiles...)
			for _, path := range all {
				fullpath := inst.Abs(path)

				stat, err := os.Stat(fullpath)
				if err != nil {
					return err
				}

				b, err := ioutil.ReadFile(fullpath)
				if err != nil {
					return err
				}

				opts := []format.Option{}
				if *fSimplify {
					opts = append(opts, format.Simplify())
				}

				b, err = format.Source(b, opts...)
				if err != nil {
					return err
				}

				err = ioutil.WriteFile(fullpath, b, stat.Mode())
				if err != nil {
					return err
				}
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(fmtCmd)
}
