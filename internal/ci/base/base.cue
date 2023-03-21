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

// package base is a collection of workflows, jobs, stes etc that are common to
// CUE projects and the workflows they specify. The package itself needs to be
// instantiated to parameterise many of the exported definitions.
//
// For example a package using base would do something like this:
//
//     package workflows
//
//     import "cuelang.org/go/internal/ci/base"
//
//     // Create an instance of base
//     _base: base & {
//         #repositoryURL:                core.#githubRepositoryURL
//         #defaultBranch:                core.#defaultBranch
//         #botGitHubUser:                "cueckoo"
//         #botGitHubUserTokenSecretsKey: "CUECKOO_GITHUB_PAT"
//     }
//
package base

import (
	"strings"
	"path"
	"strconv"
	encjson "encoding/json"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// Package parameters
#repositoryURL:                string
#defaultBranch:                string
#testDefaultBranch:            "ci/test"
#botGitHubUser:                string
#botGitHubUserTokenSecretsKey: string
#protectedBranchPatterns: [...string]
#releaseTagPattern: string

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

#checkoutCode: {
	#trailers: [...string]

	[
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
			run:  "touch -t 202211302355 $(find * -type d)"
		},
		json.#step & {
			name: "Restore git file modification times"
			uses: "chetan/git-restore-mtime-action@075f9bc9d159805603419d50f794bd9f33252ebe"
		},

		for trailer in #trailers {
			let stepName = strings.Replace(trailer, "-", "", -1)
			json.#step & {
				id:  stepName
				run: """
					x="$(git log -1 --pretty='%(trailers:key=\(trailer),valueonly)')"
					if [ "$x" == "" ]
					then
						x=null
					fi
					echo "value<<EOD" >> $GITHUB_OUTPUT
					echo "$x" >> $GITHUB_OUTPUT
					echo "EOD" >> $GITHUB_OUTPUT
					"""
			}
		},
	]
}

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
			\#(#curlGitHubAPI) -f --request POST --data-binary \#(strconv.Quote(encjson.Marshal(#arg))) https://api.github.com/repos/\#(_#repositoryPath)/dispatches
			"""#
}

#curlGitHubAPI: #"""
	curl -s -L -H "Accept: application/vnd.github+json" -H "Authorization: Bearer ${{ secrets.\#(#botGitHubUserTokenSecretsKey) }}" -H "X-GitHub-Api-Version: 2022-11-28"
	"""#

#setupGoActionsCaches: {
	// #protectedBranchExpr is a GitHub expression
	// (https://docs.github.com/en/actions/learn-github-actions/expressions)
	// that evaluates to true if the workflow is running for a commit against a
	// protected branch.
	#protectedBranchExpr: string

	let goModCacheDirID = "go-mod-cache-dir"
	let goCacheDirID = "go-cache-dir"

	// cacheDirs is a convenience variable that includes
	// GitHub expressions that represent the directories
	// that participate in Go caching.
	let cacheDirs = [ "${{ steps.\(goModCacheDirID).outputs.dir }}/cache/download", "${{ steps.\(goCacheDirID).outputs.dir }}"]

	// pre is the list of steps required to establish and initialise the correct
	// caches for Go-based workflows.
	pre: [
		json.#step & {
			name: "Get go mod cache directory"
			id:   goModCacheDirID
			run:  #"echo "dir=$(go env GOMODCACHE)" >> ${GITHUB_OUTPUT}"#
		},
		json.#step & {
			name: "Get go build/test cache directory"
			id:   goCacheDirID
			run:  #"echo "dir=$(go env GOCACHE)" >> ${GITHUB_OUTPUT}"#
		},
		for _, v in [
			{
				if:   #protectedBranchExpr
				uses: "actions/cache@v3"
			},
			{
				if:   "! \(#protectedBranchExpr)"
				uses: "actions/cache/restore@v3"
			},
		] {
			v & json.#step & {
				with: {
					path: strings.Join(cacheDirs, "\n")

					// GitHub actions caches are immutable. Therefore, use a key which is
					// unique, but allow the restore to fallback to the most recent cache.
					// The result is then saved under the new key which will benefit the
					// next build
					key:            "${{ runner.os }}-${{ matrix.go-version }}-${{ github.run_id }}"
					"restore-keys": "${{ runner.os }}-${{ matrix.go-version }}"
				}
			}
		},
	]

	// post is the list of steps that need to be run at the end of
	// a workflow to "tidy up" following the earlier pre
	post: [
		json.#step & {
			let qCacheDirs = [ for v in cacheDirs {"'\(v)'"}]
			run: "find \(strings.Join(qCacheDirs, " ")) -type f -amin +7200 -delete -print"
		},
	]
}

// #codeReview defines the schema of a codereview.cfg file that
// sits at the root of a repository. codereview.cfg is the configuration
// file that drives golang.org/x/review/git-codereview. This config
// file is also used by github.com/cue-sh/tools/cmd/cueckoo.
#codeReview: {
	gerrit?:      string
	github?:      string
	"cue-unity"?: string
}

// #toCodeReviewCfg converts a #codeReview instance to
// the key: value
#toCodeReviewCfg: {
	#input: #codeReview
	let parts = [ for k, v in #input {k + ": " + v}]

	// Per https://pkg.go.dev/golang.org/x/review/git-codereview#hdr-Configuration
	strings.Join(parts, "\n")
}

// _#matchPattern returns a GitHub Actions expression which evaluates whether a
// variable matches a globbing pattern. For literal patterns it uses "==",
// and for suffix patterns it uses "startsWith".
// See https://docs.github.com/en/actions/learn-github-actions/expressions.
_#matchPattern: {
	variable: string
	pattern:  string
	expr:     [
			if strings.HasSuffix(pattern, "*") {
			let prefix = strings.TrimSuffix(pattern, "*")
			"startsWith(\(variable), '\(prefix)')"
		},
		{
			"\(variable) == '\(pattern)'"
		},
	][0]
}

// #isProtectedBranch is an expression that evaluates to true if the
// job is running as a result of pushing to one of _#protectedBranchPatterns.
// Note that use of this expression requires the existence of steps that
// test whether the provided #trailers have been set on the commit under test.
#isProtectedBranch: {
	#trailers: [...string]
	"(" + strings.Join([
		"(" + strings.Join([ for branch in #protectedBranchPatterns {
			(_#matchPattern & {variable: "github.ref", pattern: "refs/heads/\(branch)"}).expr
		}], " || ") + ")",
		if len(#trailers) > 0 {
			"(" + strings.Join([
				for trailer in #trailers {
					let stepName = strings.Replace(trailer, "-", "", -1)
					"fromJSON(steps.\(stepName).outputs.value) == null"
				},
			], " && ") + ")"
		},
	], " && ") + ")"
}

// #isReleaseTag creates a GitHub expression, based on the given release tag
// pattern, that evaluates to true if called in the context of a workflow that
// is part of a release.
#isReleaseTag: {
	(_#matchPattern & {variable: "github.ref", pattern: "refs/tags/\(#releaseTagPattern)"}).expr
}

// #trybotKey and #unityKey are defined by github.com/cue-sh/tools/cmd/cueckoo.
// They are used as keys across various CUE workflows and their consistency in
// those various locations is therefore crucial.
#trybotKey: "trybot"
#unityKey:  "unity"

let _trailerSuffix = "-Trailer"
#TrybotTrailer: #trybotKey + _trailerSuffix
#UnityTrailer:  #unityKey + _trailerSuffix
