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

package ci

import (
	"path"
	"encoding/yaml"
	"tool/file"

	"cuelang.org/go/internal/ci/repo"
	"cuelang.org/go/internal/ci/github"
)

// For the commands below, note we use simple yet hacky path resolution, rather
// than anything that might derive the module root using go list or similar, in
// order that we have zero dependencies.  This is important because this CUE
// package is "vendored" to an external dependency so that that unity can
// re-run and verify these steps as part of a the test suite that runs against
// new CUE versions.

_goos: string @tag(os,var=os)

// gen.workflows regenerates the GitHub workflow Yaml definitions.
//
// See internal/ci/gen.go for details on how this step fits into the sequence
// of generating our CI workflow definitions, and updating various txtar tests
// with files from that process.
command: gen: {
	_dir: path.FromSlash("../../.github/workflows", path.Unix)

	workflows: {
		remove: {
			glob: file.Glob & {
				glob: path.Join([_dir, "*.yml"], _goos)
				files: [...string]
			}
			for _, _filename in glob.files {
				"delete \(_filename)": file.RemoveAll & {
					path: _filename
				}
			}
		}
		for _workflowName, _workflow in github.workflows {
			let _filename = _workflowName + repo.workflowFileExtension
			"generate \(_filename)": file.Create & {
				$after: [for v in remove {v}]
				filename: path.Join([_dir, _filename], _goos)
				let donotedit = repo.doNotEditMessage & {#generatedBy: "internal/ci/ci_tool.cue", _}
				contents: "# \(donotedit)\n\n\(yaml.Marshal(_workflow))"
			}
		}
	}
}

command: gen: codereviewcfg: file.Create & {
	_dir: path.FromSlash("../../", path.Unix)
	filename: path.Join([_dir, "codereview.cfg"], _goos)
	let res = repo.toCodeReviewCfg & {#input: repo.codeReview, _}
	let donotedit = repo.doNotEditMessage & {#generatedBy: "internal/ci/ci_tool.cue", _}
	contents: "# \(donotedit)\n\n\(res)\n"
}
