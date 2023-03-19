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

// package core is a collection of features that are common to CUE projects and
// the workflows they specify.
package base

import (
	"strings"
	"path"
	"strconv"
	encjson "encoding/json"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#repositoryURL:                string
#defaultBranch:                string
#testDefaultBranch:            "ci/test"
#botGitHubUser:                string
#botGitHubUserTokenSecretsKey: string

#doNotEditMessage: {
	#generatedBy: string
	"Code generated \(#generatedBy); DO NOT EDIT."
}

#bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

#installGo: json.#step & {
	name: "Install Go"
	uses: "actions/setup-go@v3"
	with: {
		"go-version": *"${{ matrix.go-version }}" | string
	}
}

#checkoutCode: [
	json.#step & {
		name: "Checkout code"
		uses: "actions/checkout@v3"

		// "pull_request" builds will by default use a merge commit,
		// testing the PR's HEAD merged on top of the master branch.
		// For consistency with Gerrit, avoid that merge commit entirely.
		// This doesn't affect builds by other events like "push",
		// since github.event.pull_request is unset so ref remains empty.
		with: {
			ref:           "${{ github.event.pull_request.head.sha }}"
			"fetch-depth": 0 // see the docs below
		}
	},
	// Restore modified times to work around https://go.dev/issues/58571,
	// as otherwise we would get lots of unnecessary Go test cache misses.
	// Note that this action requires actions/checkout to use a fetch-depth of 0.
	// Since this is a third-party action which runs arbitrary code,
	// we pin a commit hash for v2 to be in control of code updates.
	// Also note that git-restore-mtime does not update all directories,
	// per the bug report at https://github.com/MestreLion/git-tools/issues/47,
	// so we first reset all directory timestamps to a static time as a fallback.
	// TODO(mvdan): May be unnecessary once the Go bug above is fixed.
	json.#step & {
		name: "Reset git directory modification times"
		run:  "find . -not -path '*/.*' -type d -exec touch -t 202211302355 {} ';'"
	},
	json.#step & {
		name: "Restore git file modification times"
		uses: "chetan/git-restore-mtime-action@075f9bc9d159805603419d50f794bd9f33252ebe"
	},
]

#earlyChecks: json.#step & {
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
}

#checkGitClean: json.#step & {
	name: "Check that git is clean at the end of the job"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

let _#repositoryURL = #repositoryURL
let _#botGitHubUser = #botGitHubUser
let _#botGitHubUserTokenSecretsKey = #botGitHubUserTokenSecretsKey

#repositoryDispatch: json.#step & {
	#repositoryURL:                *_#repositoryURL | string
	#botGitHubUser:                *_#botGitHubUser | string
	#botGitHubUserTokenSecretsKey: *_#botGitHubUserTokenSecretsKey | string
	#arg:                          _

	// Pending a nicer fix in cuelang.org/issue/1433
	let _#repositoryURLNoScheme = strings.Split(#repositoryURL, "//")[1]
	let _#repositoryPath = path.Base(path.Dir(_#repositoryURLNoScheme)) + "/" + path.Base(_#repositoryURLNoScheme)

	name: string
	run:  #"""
			\#(#curl) -H "Content-Type: application/json" -u \#(#botGitHubUser):${{ secrets.\#(#botGitHubUserTokenSecretsKey) }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(#arg))) https://api.github.com/repos/\#(_#repositoryPath)/dispatches
			"""#
}

#curl: "curl -f -s"
