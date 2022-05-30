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
)

// _cueCmd defines the command that is run to run cmd/cue.
// This is factored out in order that the cue-github-actions
// project which "vendors" the various workflow-related
// packages can specify "cue" as the value so that unity
// tests can specify the cmd/cue binary to use.
_cueCmd: string | *"go run cuelang.org/go/cmd/cue@v0.4.3" @tag(cue_cmd)

// For the commands below, note we use simple yet hacky path resolution, rather
// than anything that might derive the module root using go list or similar, in
// order that we have zero dependencies.  This is important because this CUE
// package is "vendored" to an external dependency so that that unity can
// re-run and verify these steps as part of a the test suite that runs against
// new CUE versions.

// vendorgithubschema vendors the JSONSchema that defines GitHub workflows into
// the main module's cue.mod/pkg.
//
// See internal/ci/gen.go for details on how this step fits into the sequence
// of generating our CI workflow definitions, and updating various txtar tests
// with files from that process.
command: vendorgithubschema: {
	getJSONSchema: http.Get & {
		request: body: ""

		// Tip link for humans:
		// https://github.com/SchemaStore/schemastore/blob/master/src/schemas/json/github-workflow.json
		url: "https://raw.githubusercontent.com/SchemaStore/schemastore/6fe4707b9d1c5d45cfc8d5b6d56968e65d2bdc38/src/schemas/json/github-workflow.json"
	}
	// vendorGitHubWorkflowSchema writes the imported schema to the cue.mod/pkg
	// hierarchy for the GitHub workflow package. This vendored
	// package is then referenced in the internal/ci package
	// when defining workflows.
	vendorGitHubWorkflowSchema: file.Create & {
		_path:    path.FromSlash("../../cue.mod/pkg/github.com/SchemaStore/schemastore/src/schemas/json/github-workflow.json", "unix")
		filename: path.Join([_path], _#goos)
		contents: getJSONSchema.response.body
	}
}

// importjsonschema imports the "vendored" JSON schema that defines GitHub
// workflows into CUE
command: importjsonschema: {
	importJSONSchema: exec.Run & {
		_inpath:  path.FromSlash("../../cue.mod/pkg/github.com/SchemaStore/schemastore/src/schemas/json/github-workflow.json", "unix")
		_outpath: path.FromSlash("../../cue.mod/pkg/github.com/SchemaStore/schemastore/src/schemas/json/github-workflow.cue", "unix")
		cmd:      "\(_cueCmd) import -f -p json -l #Workflow: -o \(_outpath) jsonschema: \(_inpath)"
	}
}

_#goos: string @tag(goos,var=os)
