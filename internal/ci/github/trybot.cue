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
			"runs-on": "${{ matrix.runner }}"

			let _setupGoActionsCaches = _repo.setupGoActionsCaches & {
				#goVersion: goVersionVal
				#os:        runnerOSVal
				_
			}

			// Only run the trybot workflow if we have the trybot trailer, or
			// if we have no special trailers. Note this condition applies
			// after and in addition to the "on" condition above.
			if: "\(_repo.containsTrybotTrailer) || ! \(_repo.containsDispatchTrailer)"

			steps: [
				for v in _repo.checkoutCode {v},

				_repo.installGo & {
					with: "go-version": goVersionVal
				},

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _setupGoActionsCaches {v},

				_repo.earlyChecks & {
					// These checks don't vary based on the Go version or OS,
					// so we only need to run them on one of the matrix jobs.
					if: _isLatestLinux
				},
				json.#step & {
					if:  "\(_repo.isProtectedBranch) || \(_isLatestLinux)"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_goGenerate,
				_goTest & {
					if: "\(_repo.isProtectedBranch) || !\(_isLatestLinux)"
				},
				_goTestRace & {
					if: _isLatestLinux
				},
				for v in _e2eTestSteps {v},
				_goCheck,
				_repo.checkGitClean,
			]
		}
	}

	let runnerOS = "runner.os"
	let runnerOSVal = "${{ \(runnerOS) }}"
	let matrixRunner = "matrix.runner"
	let goVersion = "matrix.go-version"
	let goVersionVal = "${{ \(goVersion) }}"

	_testStrategy: {
		"fail-fast": false
		matrix: {
			"go-version": [_repo.previousStableGo, _repo.latestStableGo]
			runner: [_repo.linuxMachine, _repo.macosMachine, _repo.windowsMachine]
		}
	}

	// _isLatestLinux returns a GitHub expression that evaluates to true if the job
	// is running on Linux with the latest version of Go. This expression is often
	// used to run certain steps just once per CI workflow, to avoid duplicated
	// work.
	_isLatestLinux: "(\(goVersion) == '\(_repo.latestStableGo)' && \(matrixRunner) == '\(_repo.linuxMachine)')"

	_goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: _isLatestLinux
	}

	_goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_e2eTestSteps: [... json.#step & {
		// The end-to-end tests require a github token secret and are a bit slow,
		// so we only run them on pushes to protected branches and on one
		// environment in the source repo.
		if: "github.repository == '\(_repo.githubRepositoryPath)' && \(_repo.isProtectedBranch) && \(_isLatestLinux)"
	}] & [
		// Two setup steps per the upstream docs:
		// https://github.com/google-github-actions/setup-gcloud#service-account-key-json
		{
			name: "gcloud auth for end-to-end tests"
			id:   "auth"
			uses: "google-github-actions/auth@v2"
			// E2E_GCLOUD_KEY is a key for the service account cue-e2e-ci,
			// which has the Artifact Registry Repository Administrator role.
			with: credentials_json: "${{ secrets.E2E_GCLOUD_KEY }}"
		},
		{
			name: "gcloud setup for end-to-end tests"
			uses: "google-github-actions/setup-gcloud@v2"
		},
		{
			name: "End-to-end test"
			// The secret is the fine-grained access token "cue-lang/cue ci e2e for modules-testing"
			// owned by the porcuepine bot account with read+write access to repo administration and code
			// on the entire cue-labs-modules-testing org. Note that porcuepine is also an org admin,
			// since otherwise the repo admin access to create and delete repos does not work.
			env: GITHUB_TOKEN: "${{ secrets.E2E_GITHUB_TOKEN }}"
			run: """
				cd internal/e2e
				go test
				"""
		},
	]

	_goCheck: json.#step & {
		// These checks can vary between platforms, as different code can be built
		// based on GOOS and GOARCH build tags.
		// However, CUE does not have any such build tags yet, and we don't use
		// dependencies that vary wildly between platforms.
		// For now, to save CI resources, just run the checks on one matrix job.
		// We loop over all Go modules and use a subshell to run the commands in each directory;
		// note that this still makes the script stop at the first command failure.
		// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
		if:   "\(_isLatestLinux)"
		name: "Check"
		run: """
			for module in . internal/e2e; do
				(
					cd $module
					go vet ./...
					go mod tidy
				)
			done
			"""
	}

	_goTestRace: json.#step & {
		name: "Test with -race"
		env: GORACE: "atexit_sleep_ms=10" // Otherwise every Go package being tested sleeps for 1s; see https://go.dev/issues/20364.
		run: "go test -race ./..."
	}
}
