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
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"cuelang.org/go/cue/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var _ = errors.Print

var update = flag.Bool("update", false, "update the test files")

func printConfig(t *testing.T) *errors.Config {
	t.Helper()

	inTest = true

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return &errors.Config{
		Cwd:     cwd,
		ToSlash: true,
	}
}

func runCommand(t *testing.T, cmd *cobra.Command, name string, args ...string) {
	t.Helper()
	log.SetFlags(0)

	const dir = "./testdata"

	cfg := printConfig(t)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		t.Run(path, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			if !info.IsDir() || dir == path {
				return
			}
			testfile := filepath.Join(path, name+".out")
			bWant, err := ioutil.ReadFile(testfile)
			if err != nil {
				// Don't write the file if it doesn't exist, even in *update
				// mode. We don't want to need to support all commands for all
				// directories. Touch the file and use *update to create it.
				return
			}

			cmd.SetArgs(append(args, "./"+path))
			rOut, wOut := io.Pipe()
			cmd.SetOutput(wOut)
			var bOut []byte
			g := errgroup.Group{}
			g.Go(func() error {
				defer wOut.Close()
				defer func() {
					switch err := recover().(type) {
					case nil:
					case panicError:
						errors.Print(wOut, err.Err, cfg)
					case error:
						errors.Print(wOut, err, cfg)
					default:
						fmt.Fprintln(wOut, err)
					}
				}()
				return cmd.Execute()
			})
			g.Go(func() error {
				bOut, err = ioutil.ReadAll(rOut)
				re := regexp.MustCompile(`exit status \d`)
				bOut = re.ReplaceAll(bOut, []byte("non-zero exist code"))
				return err
			})
			if err := g.Wait(); err != nil {
				t.Error(err)
			}
			if *update {
				_ = ioutil.WriteFile(testfile, bOut, 0644)
				return
			}
			got, want := string(bOut), string(bWant)
			if got != want {
				t.Errorf("\n got: %v\nwant: %v", got, want)
			}
		})
		return nil
	})
}

func TestLoadError(t *testing.T) {
	runCommand(t, newEvalCmd(), "loaderr", "non-existing", ".")
}
