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
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The test workflow.
trybot: _core.#bashWorkflow & {
	// Note: the name of this workflow is used by gerritstatusupdater in the
	// status updates that are posted as reviews for this workflows.
	name: "trybot"

	on: {
		push: {
			branches: ["**"] // any branch (including '/' namespaced branches)
			"tags-ignore": [_#releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_core.#installGo,
				_core.#checkoutCode & {
					// "pull_request" builds will by default use a merge commit,
					// testing the PR's HEAD merged on top of the master branch.
					// For consistency with Gerrit, avoid that merge commit entirely.
					// This doesn't affect "push" builds, which never used merge commits.
					with: ref: "${{ github.event.pull_request.head.sha }}"
				},
				_core.#earlyChecks & {
					// These checks don't vary based on the Go version or OS,
					// so we only need to run them on one of the matrix jobs.
					if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
				},
				_core.#cacheGoModules,
				json.#step & {
					if:  "${{ \(_core.#isDefaultBranch) }}"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_#goGenerate,
				_#goTest,
				_#goCheck,
				_#goTestRace & {
					if: "${{ matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)' }}"
				},
				_core.#checkGitClean,
				_#pullThroughProxy,
			]
		}
	}

	_#pullThroughProxy: json.#step & {
		name: "Pull this commit through the proxy on \(_#defaultBranch)"
		run: """
			v=$(git rev-parse HEAD)
			cd $(mktemp -d)
			go mod init mod.com
			GOPROXY=https://proxy.golang.org go get -d cuelang.org/go/cmd/cue@$v
			"""
		if: "${{ \(_core.#isDefaultBranch) }}"
	}

	_#goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
	}

	_#goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_#goCheck: json.#step & {
		// These checks can vary between platforms, as different code can be built
		// based on GOOS and GOARCH build tags.
		// However, CUE does not have any such build tags yet, and we don't use
		// dependencies that vary wildly between platforms.
		// For now, to save CI resources, just run the checks on one matrix job.
		// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
		if:   "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
		name: "Check"
		run:  "go vet ./..."
	}

	_#goTestRace: json.#step & {
		name: "Test with -race"
		run:  "go test -race ./..."
	}
}
