// Copyright 2021 The CUE Authors
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

package vendor

import (
	"path"

	"tool/exec"
	"tool/file"
	"tool/http"
	"tool/os"
)

// _#modroot is a common helper to get the module root
//
// TODO: use once we have a solution to cuelang.org/issue/704.
// This will then allow us to remove the use of .. below.
_#modroot: exec.Run & {
	cmd:    "go list -m -f {{.Dir}}"
	stdout: string
}

// Until we have the ability to inject contextual information
// we need to pass in GOOS explicitly. Either by environment
// variable (which we get for free when this is used via go generate)
// or via a tag in the case you want to manually run the CUE
// command.
_#goos: os.Getenv & {
	GOOS: *"unix" | string @tag(os)
}

// vendorgithubschema vendors a "cue import"-ed version of the JSONSchema that
// defines GitHub workflows into the main module's cue.mod/pkg.
//
// Until we have a resolution for cuelang.org/issue/704 this must be
// run from the internal/ci package. At which point we can switch to using
// _#modroot and move the associated go:generate directive into
// internal/ci alongside this command definition.
//
// This also explains why the ../../ relative path specification below
// appear wrong in the context of the containing directory internal/ci/vendor.
command: vendorgithubschema: {
	goos: _#goos
	get:  http.Get & {
		request: body: ""

		// Tip link for humans:
		// https://github.com/SchemaStore/schemastore/blob/master/src/schemas/json/github-workflow.json
		url: "https://raw.githubusercontent.com/SchemaStore/schemastore/6fe4707b9d1c5d45cfc8d5b6d56968e65d2bdc38/src/schemas/json/github-workflow.json"
	}
	convert: exec.Run & {
		stdin:  get.response.body
		cmd:    "go run cuelang.org/go/cmd/cue import -f -p json -l #Workflow: jsonschema: - -o -"
		stdout: string
	}
	write: file.Create & {
		_path:    path.FromSlash("../../cue.mod/pkg/github.com/SchemaStore/schemastore/src/schemas/json/github-workflow.cue", "unix")
		filename: path.Join([_path], goos.GOOS)
		contents: convert.stdout
	}
}
