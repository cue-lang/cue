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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/encoding/gotypes"
	"cuelang.org/go/internal/filetypes"

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

	// Commands to some day promote out of `cue exp`.
	cmd.AddCommand(newExpGenGoTypesCmd(c))

	// Commands which are never meant to be promoted out of `cue exp`.
	cmd.AddCommand(newExpWritefsCmd(c))

	// Hidden commands which are only meant for integration tests.
	cmd.AddCommand(&cobra.Command{
		// Hang forever, disregarding context cancellation when SIGINT is received.
		// Used to test that cmd/cue still exits in such a scenario.
		Use:    "internal-hang",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// We don't do e.g. an empty select, as that can cause the runtime
			// to panic due to the detected deadlock.
			time.Sleep(time.Hour)
		},
	})
	return cmd
}

func newExpGenGoTypesCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gengotypes",
		Short: "generate Go types from CUE definitions",
		Long: `
WARNING: THIS COMMAND IS EXPERIMENTAL.

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

func newExpWritefsCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "writefs",
		Short: "remove and create files in bulk",
		// NOTE: any changes to the schema below must be made to internal/ci/base/write.cue too.
		Long: `
WARNING: THIS COMMAND IS EXPERIMENTAL.
In the future, it will be entirely replaced by native @export(...)
as described in https://cuelang.org/cl/issue/2031.

writefs takes JSON via stdin in the form of

	// tool is the name of the tool that initiated the writefs call.
	tool!: string

	// remove is a list of glob patterns of files to remove before creating new ones.
	remove?: [...string]

	// create is the set of files to create, keyed by Unix file paths.
	create?: [filepath=string]: {
		type!:     "symlink"
		contents!: string
	} | *{
		type: "file"

		// encoding can be set to a filetype like "text", "json", or "yaml"
		// to control how the arbitrary concrete value in contents is encoded.
		// When unset, the filepath extension is used to infer an encoding,
		// with a fallback to "text" for filepaths with no extension.
		encoding?: string
		contents!: _
	}

For example, this tool can be used via "cue cmd" as follows:

	command: gen: exec.Run & {
		cmd: ["cue", "exp", "writefs"]
		stdin: json.Marshal({
			tool: "cue cmd gen"
			remove: ["out/*.yaml"]
			create: {
				"out/foo.yaml": {contents: {body: "some struct"}}
				"out/bar.yaml": {contents: [some list]}
			}
		})
	}
`[1:],
		RunE: mkRunE(c, runExpWritefs),
	}

	return cmd
}

type writefsFile struct {
	Type     string          `json:"type"`
	Encoding string          `json:"encoding"`
	Contents json.RawMessage `json:"contents"`
}

type writefsArgs struct {
	Tool   string                 `json:"tool"`
	Remove []string               `json:"remove"`
	Create map[string]writefsFile `json:"create"`
}

func runExpWritefs(cmd *Command, args []string) error {
	var todo writefsArgs
	dec := json.NewDecoder(cmd.InOrStdin())
	if err := dec.Decode(&todo); err != nil {
		return fmt.Errorf("failed to decode arguments from stdin: %v", err)
	}
	for _, glob := range todo.Remove {
		files, err := filepath.Glob(glob)
		if err != nil {
			return fmt.Errorf("failed to glob %q: %v", glob, err)
		}
		if len(files) == 0 {
			continue
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				return fmt.Errorf("failed to remove %s: %v", f, err)
			}
		}
	}

	ctx := cuecontext.New()

	for fp, f := range todo.Create {
		fp = filepath.FromSlash(fp)
		dir := filepath.Dir(fp)
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return fmt.Errorf("failed to mkdir %s: %v", dir, err)
		}
		switch f.Type {
		case "symlink":
			// f.Contents must be a string
			var contents string
			if err := json.Unmarshal(f.Contents, &contents); err != nil {
				return fmt.Errorf("%s: Type is symlink, but Contents not a string type", fp)
			}
			target := filepath.FromSlash(contents)
			actualTarget, err := os.Readlink(fp)
			if err == nil && actualTarget == target {
				continue
			}
			if err := os.Symlink(target, fp); err != nil {
				return fmt.Errorf("failed to symlink %s -> %s: %v", fp, target, err)
			}
		case "file", "": // empty if omitted, as it's the default
			fenc := f.Encoding
			if fenc == "" && filepath.Ext(fp) == "" {
				// Fall back to text when there is no encoding nor extension, which is a useful default
				// without causing issues when we add more encodings with extensions in the future.
				fenc = "text"
			}
			bfile, err := filetypes.ParseFileAndType(fp, fenc, filetypes.Export)
			if err != nil {
				return fmt.Errorf("failed to infer filetype: %v", err)
			}

			var buf bytes.Buffer
			// Gross: hack in a "code generated by" header for filetypes that support comments
			//
			// TODO: replace this gross hack once we can encode comments.
			switch bfile.Encoding {
			case build.YAML, build.TOML:
				fmt.Fprintf(&buf, "# Code generated %s; DO NOT EDIT.\n\n", todo.Tool)
			}

			enc, err := encoding.NewEncoder(cmd.ctx, bfile, &encoding.Config{Out: &buf})
			if err != nil {
				return fmt.Errorf("failed to create encoder: %v", err)
			}

			v := ctx.CompileBytes(f.Contents)
			if err := enc.Encode(v); err != nil {
				return fmt.Errorf("failed to encode file: %v", err)
			}
			if err := enc.Close(); err != nil {
				return fmt.Errorf("failed to encode file: %v", err)
			}
			if err := os.WriteFile(fp, buf.Bytes(), 0o666); err != nil {
				return fmt.Errorf("failed to write file %s: %v", fp, err)
			}
		default:
			return fmt.Errorf("invalid type: %q", f.Type)
		}
	}
	return nil
}
