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

	"cuelang.org/go/internal/ci/core"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The trybot workflow.
workflows: trybot: core.bashWorkflow & {
	name: core.trybot.name

	// Declare an instance of _#isProtectedBranch for use in this workflow
	let _isProtectedBranch = core.isProtectedBranch & {
		#trailers: [core.trybot.trailer]
		_
	}

	on: {
		push: {
			branches: list.Concat([[core.testDefaultBranch], core.protectedBranchPatterns]) // do not run PR branches
			"tags-ignore": [core.releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			strategy:  _testStrategy
			"runs-on": "${{ matrix.os }}"

			let goCaches = core.setupGoActionsCaches & {#protectedBranchExpr: _isProtectedBranch, _}

			let checkoutCode = core.checkoutCode & {#trailers: [core.trybot.trailer], _}

			steps: [
				for v in checkoutCode {v},

				core.installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in goCaches {v},

				// All tests on protected branches should skip the test cache.
				// The canonical way to do this is with -count=1. However, we
				// want the resulting test cache to be valid and current so that
				// subsequent CLs in the trybot repo can leverage the updated
				// cache. Therefore, we instead perform a clean of the testcache.
				json.#step & {
					if:  "github.repository == '\(core.githubRepositoryPath)' && (\(core.isProtectedBranch) || github.ref == 'refs/heads/\(core.testDefaultBranch)')"
					run: "go clean -testcache"
				},

				core.earlyChecks & {
					// These checks don't vary based on the Go version or OS,
					// so we only need to run them on one of the matrix jobs.
					if: core.isLatestLinux
				},
				json.#step & {
					if:  "\(_isProtectedBranch) || \(core.isLatestLinux)"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_goGenerate,
				_goTest & {
					if: "\(_isProtectedBranch) || !\(core.isLatestLinux)"
				},
				_goTestRace & {
					if: core.isLatestLinux
				},
				_goCheck,
				core.checkGitClean,
				_pullThroughProxy,
			]
		}
	}

	_testStrategy: {
		"fail-fast": false
		matrix: {
			"go-version": ["1.18.x", core.latestStableGo]
			os: [core.linuxMachine, core.macosMachine, core.windowsMachine]
		}
	}

	_pullThroughProxy: json.#step & {
		name: "Pull this commit through the proxy on \(core.defaultBranch)"
		run: """
			v=$(git rev-parse HEAD)
			cd $(mktemp -d)
			go mod init test

			# Try up to five times if we get a 410 error, which either the proxy or sumdb
			# can return if they haven't retrieved the requested version yet.
			for i in {1..5}; do
				# GitHub Actions defaults to "set -eo pipefail", so we use an if clause to
				# avoid stopping too early. We also use a "failed" file as "go get" runs
				# in a subshell via the pipe.
				rm -f failed
				if ! GOPROXY=https://proxy.golang.org go get cuelang.org/go@$v; then
					touch failed
				fi |& tee output.txt

				if [[ -f failed ]]; then
					if grep -q '410 Gone' output.txt; then
						echo "got a 410; retrying"
						sleep 1s # do not be too impatient
						continue
					fi
					exit 1 # some other failure; stop
				else
					exit 0 # success; stop
				fi
			done

			echo "giving up after a number of retries"
			exit 1
			"""
		if: "\(_isProtectedBranch) && \(core.isLatestLinux)"
	}

	_goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: core.isLatestLinux
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
		if:   "\(core.isLatestLinux)"
		name: "Check"
		run:  "go vet ./..."
	}

	_goTestRace: json.#step & {
		name: "Test with -race"
		run:  "go test -race ./..."
	}
}
