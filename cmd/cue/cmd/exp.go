// Copyright 2024 CUE Authors
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
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/encoding/gotypes"

	"github.com/spf13/cobra"
)

func newExpCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		// Experimental commands are hidden by design.
		Hidden: true,

		Use:   "exp <cmd> [arguments]",
		Short: "experimental commands",
		Long: `
exp groups commands which are still in an experimental stage.

Experimental commands may be changed or removed at any time,
as the objective is to gain experience and then move the feature elsewhere.
`[1:],
	})

	cmd.AddCommand(newExpGenGoTypesCmd(c))
	cmd.AddCommand(newExpDatacmpCmd(c))
	return cmd
}

func newExpGenGoTypesCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gengotypes",
		Short: "generate Go types from CUE definitions",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

gengotypes generates Go type definitions from exported CUE definitions.

The generated Go types are guaranteed to accept any value accepted by the CUE definitions,
but may be more general. For example, "string | int" will translate into the Go
type "any" because the Go type system is not able to express disjunctions.

To ensure that the resulting Go code works, any imported CUE packages or
referenced CUE definitions are transitively generated as well.
Code is generated in each CUE package directory at cue_types_${pkgname}_gen.go,
where the package name is omitted from the filename if it is implied by the import path.

Generated Go type and field names may differ from the original CUE names by default.
For instance, an exported definition "#foo" becomes "Foo",
and a nested definition like "#foo.#bar" becomes "Foo_Bar".

@go attributes can be used to override which name to be generated:

	package foo
	@go(betterpkgname)

	#Bar: {
		@go(BetterBarTypeName)
		renamed: int @go(BetterFieldName)
	}

The attribute "@go(-)" can be used to ignore a definition or field:

	#ignoredDefinition: {
		@go(-)
	}
	ignoredField: int @go(-)

"type=" overrides an entire value to generate as a given Go type expression:

	retypedLocal:  [string]: int @go(,type=map[LocalType]int)
	retypedImport: [...string]   @go(,type=[]"foo.com/bar".ImportedType)

"optional=" controls how CUE optional fields are generated as Go fields.
The default is "zero", representing a missing field as the zero value.
"nillable" ensures the generated Go type can represent missing fields as nil.

	optionalDefault?:  int                         // generates as "int64"
	optionalNillable?: int @go(,optional=nillable) // generates as "*int64"
	nested: {
		@go(,optional=nillable) // set for all fields under this struct
	}
`[1:],
		RunE: mkRunE(c, runExpGenGoTypes),
	}

	return cmd
}

func runExpGenGoTypes(cmd *Command, args []string) error {
	insts := load.Instances(args, &load.Config{})
	return gotypes.Generate(cmd.ctx, insts...)
}

func newExpDatacmpCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Hidden:        true,
		SilenceErrors: true, // TODO: This doesn't appear to prevent Cobra's "exit status 1".
		SilenceUsage:  true, // TODO: Ditto.
		Use:           "datacmp <file1> <file2> ... <fileN>",
		Args:          cobra.MinimumNArgs(2),
		Short:         "assert that data files contain the same data",
		Long: `
WARNING: THIS COMMAND IS EXPERIMENTAL. It may be changed or removed at any time.

datacmp checks that any number of data files all contain exactly the same data.

It operates only on data files that have these filename extensions:

  .json .jsonl .ndjson   Files must contain JSON data
  .yaml .yml             Files must contain YAML data
  .toml                  Files must contain TOML data			
  .txt                   Files must contain text data

See "cue help filetypes" for more information on these specific encodings.

CUE files are not supported. The "cue vet" command uses unification to help
check that multiple CUE files contain the same data.
`[1:],
		RunE: mkRunE(c, runExpDatacmpCmd),
	}

	return cmd
}

// runExpDatacmpCmd runs "cue import" on each data file input parameter,
// placing each in its own definition path in a temporary file, and then runs
// "cue eval -e '#def1 & #def2 & #def3' tmp1.cue tmp2.cue tmp3.cue" to assert
// that each file contains exactly (and only) the same data.
func runExpDatacmpCmd(cmd *Command, args []string) error {
	tmpDir, err := os.MkdirTemp("", "cue-exp-datacmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for i, arg := range args {
		if stderr, err := datacmpImportDatafile(arg, tmpDir, i+1); err != nil {
			return errors.New(stderr)
		}
	}
	if stderr, err := datacmpCueEval(tmpDir, len(args)); err != nil {
		return errors.New(stderr)
	}
	return nil
}

const (
	// Used as both a .cue filename prefix and a CUE definition identifier prefix
	datacmpFilePrefix = "file"
)

func datacmpImportDatafile(dataFile, dstDir string, idx int) (stderr string, err error) {
	cueFileName := fmt.Sprintf("%s%d.cue", datacmpFilePrefix, idx)
	cueFilePath := filepath.Join(dstDir, cueFileName)
	importParam := fmt.Sprintf("#%s%d:", datacmpFilePrefix, idx)

	var b bytes.Buffer
	cmd := exec.Command("cue", "import",
		"-o", cueFilePath,
		"-l", importParam,
		dataFile)
	cmd.Stderr = &b

	err = cmd.Run()
	stderr = b.String()

	return
}

func datacmpCueEval(dir string, fileCount int) (stderr string, err error) {
	var b bytes.Buffer
	cmdArgs := append([]string{
		"eval",
		"-e", datacmpUnifiedExpression(fileCount),
		"cue:"}, datacmpFilePaths(dir, fileCount)...)
	cmd := exec.Command("cue", cmdArgs...)
	cmd.Stderr = &b

	err = cmd.Run()
	stderr = b.String()

	return
}

// datacmpUnifiedExpression returns a string like "#file1 & #file2 & ... & #filen".
func datacmpUnifiedExpression(n int) string {
	if n <= 0 {
		return ""
	}
	files := make([]string, n)
	for i := 1; i <= n; i++ {
		files[i-1] = fmt.Sprintf("#%s%d", datacmpFilePrefix, i)
	}
	return strings.Join(files, " & ")
}

// datacmpFilePaths returns a slice of file paths like "dstDir/file1", ..., "dstDir/fileN".
func datacmpFilePaths(dstDir string, fileCount int) []string {
	if fileCount <= 0 {
		return nil
	}
	paths := make([]string, fileCount)
	for i := 1; i <= fileCount; i++ {
		paths[i-1] = filepath.Join(dstDir, fmt.Sprintf("%s%d.cue", datacmpFilePrefix, i))
	}
	return paths
}
