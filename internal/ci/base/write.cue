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

package base

import (
	"path"
	"encoding/yaml"
	"tool/file"

	"cue.dev/x/githubactions"
)

// For the commands below, note we use simple yet hacky path resolution, rather
// than anything that might derive the module root using go list or similar, in
// order that we have zero dependencies.  This is important because this CUE
// package is "vendored" to an external dependency so that that unity can
// re-run and verify these steps as part of the test suite that runs against
// new CUE versions.

// TODO(mvdan): without the default, this doesn't seem to be filled by
// `cue cmd gen` in internal/ci, even though the command defaults --inject-vars to true.
// Perhaps it doesn't work for imported packages? It seems to me like it should.
_goos: string | *path.Unix @tag(os,var=os)

// uniqueWorkflowNames enforces that workflow names are unique,
// since we use them to uniquely identify workflow runs depicted in GitHub events,
// such as "TryBot" or "Unity".
uniqueWorkflowNames: self={
	_uniqueWorkflowNames: {
		for basename, workflow in self {
			(workflow.name): basename
		}
	}
}

// writeWorkflows regenerates the GitHub workflow YAML definitions.
writeWorkflows: {
	#in: {
		workflows: [string]: githubactions.#Workflow
		workflows: uniqueWorkflowNames
	}
	_dir: path.FromSlash("../../.github/workflows", path.Unix)

	remove: {
		glob: file.Glob & {
			glob: path.Join([_dir, "*" + workflowFileExtension], _goos)
			files: [...string]
		}
		for _, _filename in glob.files {
			"delete \(_filename)": file.RemoveAll & {
				path: _filename
			}
		}
	}
	for _workflowName, _workflow in #in.workflows {
		let _filename = _workflowName + workflowFileExtension
		"generate \(_filename)": file.Create & {
			$after: [for v in remove {v}]
			filename: path.Join([_dir, _filename], _goos)
			let donotedit = doNotEditMessage & {#generatedBy: "internal/ci/base/write.cue", _}
			contents: "# \(donotedit)\n\n\(yaml.Marshal(_workflow))"
		}
	}
}

writeCodereviewCfg: file.Create & {
	_dir: path.FromSlash("../../", path.Unix)
	filename: path.Join([_dir, "codereview.cfg"], _goos)
	let res = toCodeReviewCfg & {#input: codeReview, _}
	let donotedit = doNotEditMessage & {#generatedBy: "internal/ci/base/write.cue", _}
	contents: "# \(donotedit)\n\n\(res)\n"
}
