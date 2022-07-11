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

package github

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

workflowsDir: *"./" | string @tag(workflowsDir)

_#masterBranch:      "master"
_#releaseTagPattern: "v*"

workflows: [...{file: string, schema: (json.#Workflow & {})}]
workflows: [
	{
		// Note: the name of the file corresponds to the environment variable
		// names for gerritstatusupdater. Therefore, this filename must only be
		// change in combination with also updating the environment in which
		// gerritstatusupdater is running for this repository.
		file:   "trybot.yml"
		schema: trybot
	},
	{
		file:   "trybot_dispatch.yml"
		schema: trybot_dispatch
	},
	{
		file:   "release.yml"
		schema: release
	},
	{
		file:   "tip_triggers.yml"
		schema: tip_triggers
	},
]

_#bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

// TODO: drop when cuelang.org/issue/390 is fixed.
// Declare definitions for sub-schemas
_#job:  ((json.#Workflow & {}).jobs & {x: _}).x
_#step: ((_#job & {steps:                 _}).steps & [_])[0]

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
_#latestStableGo: "1.18.x"

// Use a specific latest version for release builds.
// Note that we don't want ".x" for the sake of reproducibility,
// so we instead pin a specific Go release.
_#pinnedReleaseGo: "1.18.1"

_#linuxMachine:   "ubuntu-20.04"
_#macosMachine:   "macos-11"
_#windowsMachine: "windows-2022"

_#testStrategy: {
	"fail-fast": false
	matrix: {
		"go-version": ["1.17.x", _#latestStableGo]
		os: [_#linuxMachine, _#macosMachine, _#windowsMachine]
	}
}

_#installGo: _#step & {
	name: "Install Go"
	uses: "actions/setup-go@v3"
	with: {
		"go-version": *"${{ matrix.go-version }}" | string
	}
}

_#checkoutCode: _#step & {
	name: "Checkout code"
	uses: "actions/checkout@v3"
}

_#earlyChecks: _#step & {
	name: "Early git and code sanity checks"
	run: """
		# Ensure the recent commit messages have Signed-off-by headers.
		# TODO: Remove once this is enforced for admins too;
		# see https://bugs.chromium.org/p/gerrit/issues/detail?id=15229
		# TODO: Our --max-count here is just 1, because we've made mistakes very
		# recently. Increase it to 5 or 10 soon, to also cover CL chains.
		for commit in $(git rev-list --max-count=1 HEAD); do
			if ! git rev-list --format=%B --max-count=1 $commit | grep -q '^Signed-off-by:'; then
				echo -e "\nRecent commit is lacking Signed-off-by:\n"
				git show --quiet $commit
				exit 1
			fi
		done
		"""
	// These checks don't vary based on the Go version or OS,
	// so we only need to run them on one of the matrix jobs.
	if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
}

_#cacheGoModules: _#step & {
	name: "Cache Go modules"
	uses: "actions/cache@v3"
	with: {
		path: "~/go/pkg/mod"
		key:  "${{ runner.os }}-${{ matrix.go-version }}-go-${{ hashFiles('**/go.sum') }}"
		"restore-keys": """
			${{ runner.os }}-${{ matrix.go-version }}-go-
			"""
	}
}

_#goGenerate: _#step & {
	name: "Generate"
	run:  "go generate ./..."
	// The Go version corresponds to the precise version specified in
	// the matrix. Skip windows for now until we work out why re-gen is flaky
	if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
}

_#goTest: _#step & {
	name: "Test"
	run:  "go test ./..."
}

_#goCheck: _#step & {
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

_#goTestRace: _#step & {
	name: "Test with -race"
	run:  "go test -race ./..."
}

_#checkGitClean: _#step & {
	name: "Check that git is clean post generate and tests"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

_#branchRefPrefix: "refs/heads/"

_#tempCueckooGitDir: """
	mkdir tmpgit
	cd tmpgit
	git init
	git config user.name cueckoo
	git config user.email cueckoo@gmail.com
	git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} | base64)"
	"""

_#curl: "curl -f -s"
