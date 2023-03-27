package base

// This file contains aspects principally related to GitHub workflows

import (
	encjson "encoding/json"
	"strings"
	"strconv"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

installGo: json.#step & {
	name: "Install Go"
	uses: "actions/setup-go@v4"
	with: {
		// We do our own caching in setupGoActionsCaches.
		cache:        false
		"go-version": *"${{ matrix.go-version }}" | string
	}
}

checkoutCode: {
	#actionsCheckout: json.#step & {
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
	}

	[
		#actionsCheckout,
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
	]
}

earlyChecks: json.#step & {
	name: "Early git and code sanity checks"
	run: #"""
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

		# Ensure that commit messages have a blank second line.
		# We know that a commit message must be longer than a single
		# line because each commit must be signed-off.
		if git log --format=%B -n 1 HEAD | sed -n '2{/^$/{q1}}'; then
			echo "second line of commit message must be blank"
			exit 1
		fi

		# Ensure that the commit author is the same as the signed-off-by.  This
		# is a basic requirement of DCO. It is enforced by Gerrit (although
		# noting that in Gerrit the author name does not have to match, only
		# the email address), but _not_ by the DCO GitHub app:
		#
		#   https://github.com/dcoapp/app/issues/201
		#
		# Provide a sanity check as part of GitHub workflows that should enforce
		# this, e.g. trybot workflows.
		#
		# We do so by comparing the commit author and "Signed-off-by" trailer for
		# strict equality. Whilst this is more strict than Gerrit, it should
		# generally be the case, and we can always relax this when presented with
		# specific situations where it is is a problem.

		# commit author email address
		commitauthor="$(git log -1 --pretty="%ae")"

		# signed-off-by trailer email address. There is no way to parse just the
		# email address from the trailer in the same way as git log, so instead
		# grab the relevant trailer and then take the last whitespace-delimited
		# part as the "<>" contained email address.
		# Getting the Signed-off-by trailer in this way causes blank
		# lines for some reason. Use awk to remove them.
		commitsigner="$(git log -1 --pretty='%(trailers:key=Signed-off-by,valueonly)' | sed -ne 's/.* <\(.*\)>/\1/p')"

		if [[ "$commitauthor" != "$commitsigner" ]]; then
			echo "commit author email address does not match signed-off-by trailer"
			exit 1
		fi
		"""#
}

curlGitHubAPI: #"""
	curl -s -L -H "Accept: application/vnd.github+json" -H "Authorization: Bearer ${{ secrets.\#(botGitHubUserTokenSecretsKey) }}" -H "X-GitHub-Api-Version: 2022-11-28"
	"""#

setupGoActionsCaches: {
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
	[
		// TODO: once https://github.com/actions/setup-go/issues/54 is fixed,
		// we could use `go env` outputs from the setup-go step.
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
}

// #isProtectedBranch is an expression that evaluates to true if the
// job is running as a result of pushing to one of _#protectedBranchPatterns.
// It would be nice to use the "contains" builtin for simplicity,
// but array literals are not yet supported in expressions.
isProtectedBranch: {
	"(" + strings.Join([ for branch in protectedBranchPatterns {
		(_matchPattern & {variable: "github.ref", pattern: "refs/heads/\(branch)"}).expr
	}], " || ") + ")"
}

// #isReleaseTag creates a GitHub expression, based on the given release tag
// pattern, that evaluates to true if called in the context of a workflow that
// is part of a release.
isReleaseTag: {
	(_matchPattern & {variable: "github.ref", pattern: "refs/tags/\(releaseTagPattern)"}).expr
}

checkGitClean: json.#step & {
	name: "Check that git is clean at the end of the job"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

repositoryDispatch: json.#step & {
	#githubRepositoryPath:         *githubRepositoryPath | string
	#botGitHubUser:                *botGitHubUser | string
	#botGitHubUserTokenSecretsKey: *botGitHubUserTokenSecretsKey | string
	#arg:                          _

	name: string
	run:  #"""
			\#(curlGitHubAPI) -f --request POST --data-binary \#(strconv.Quote(encjson.Marshal(#arg))) https://api.github.com/repos/\#(#githubRepositoryPath)/dispatches
			"""#
}
