// Copyright 2022 The CUE Authors
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

package github

import (
	"list"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The trybot workflow.
workflows: trybot: _repo.bashWorkflow & {
	name: _repo.trybot.name

	on: {
		push: {
			branches: list.Concat([[_repo.testDefaultBranch], _repo.protectedBranchPatterns]) // do not run PR branches
			"tags-ignore": [_repo.releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			strategy:  _testStrategy
			"runs-on": "${{ matrix.os }}"

			steps: [
				for v in _repo.checkoutCode {v},
				_repo.installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _repo.setupGoActionsCaches {v},

				_repo.earlyChecks & {
					// These checks don't vary based on the Go version or OS,
					// so we only need to run them on one of the matrix jobs.
					if: _repo.isLatestLinux
				},
				json.#step & {
					if:  "\(_repo.isProtectedBranch) || \(_repo.isLatestLinux)"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_goGenerate,
				_goTest & {
					if: "\(_repo.isProtectedBranch) || !\(_repo.isLatestLinux)"
				},
				_goTestRace & {
					if: _repo.isLatestLinux
				},
				_goCheck,
				_repo.checkGitClean,
			]
		}
	}

	_testStrategy: {
		"fail-fast": false
		matrix: {
			"go-version": ["1.19.x", _repo.latestStableGo]
			os: [_repo.linuxMachine, _repo.macosMachine, _repo.windowsMachine]
		}
	}

	_goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: _repo.isLatestLinux
	}

	_goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_goCheck: json.#step & {
		// These checks can vary between platforms, as different code can be built
		// based on GOOS and GOARCH build tags.
		// However, CUE does not have any such build tags yet, and we don't use
		// dependencies that vary wildly between platforms.
		// For now, to save CI resources, just run the checks on one matrix job.
		// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
		if:   "\(_repo.isLatestLinux)"
		name: "Check"
		run:  "go vet ./..."
	}

	_goTestRace: json.#step & {
		name: "Test with -race"
		run:  "go test -race ./..."
	}
}
