package base

// This file contains aspects principally related to GitHub workflows

import (
	"encoding/json"
	"list"
	"strings"
	"strconv"

	"cue.dev/x/githubactions"
)

bashWorkflow: githubactions.#Workflow & {
	// Use a custom default shell that extends the GitHub default to also fail
	// on access to unset variables.
	//
	// https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions#defaultsrunshell
	jobs: [string]: defaults: run: shell: "bash --noprofile --norc -euo pipefail {0}"
}

// These are useful for workflows where we use a matrix over different OS runners
// or multiple Go versions. Note that the matrix names must match.
matrixRunnerName:    "runner"
matrixRunnerExpr:    "matrix.\(matrixRunnerName)"
matrixGoVersionName: "go-version"
matrixGoVersionExpr: "matrix.\(matrixGoVersionName)"

// isLatestGoLinux is a GitHub expression that evaluates to true if the job
// is running on Linux with the latest version of Go. This expression is often
// used to run certain steps just once per CI workflow, to avoid duplicated work.
isLatestGoLinux: "(\(matrixGoVersionExpr) == '\(latestGo)' && \(matrixRunnerExpr) == '\(linuxMachine)')"

installGo: {
	#setupGo: githubactions.#Step & {
		name: "Install Go"
		uses: "actions/setup-go@v6"
		with: {
			// We do our own caching in setupCaches.
			cache: false
			// Allow overriding when using matrixGoVersionExpr.
			"go-version": string | *latestGo
		}
	}

	// Set Go env vars here with `go env -w` rather than for an entire workflow,
	// job, or per step. This keeps the logic close to where we set up Go,
	// and also prevents setting up Go but forgetting to set up the env vars too.
	//
	// Note that actions/setup-go since v6 already sets GOTOOLCHAIN=local.
	[
		#setupGo,

		githubactions.#Step & {
			name: "Set common go env vars"
			run: """
				case $(go env GOARCH) in
				amd64) go env -w GOAMD64=v3 ;;   # 2013 and later; makes `go test -race` 15% faster
				arm64) go env -w GOARM64=v8.6 ;; # Apple M2 and later
				esac

				# Dump env for good measure
				go env
				"""
		},
	]
}

checkoutCode: [...githubactions.#Step] & [
	{
		name: "Checkout code"
		uses: "actions/checkout@v4" // TODO(mvdan): switch to namespacelabs/nscloud-checkout-action@v1 once Windows supports caching

		// "pull_request_target" builds will by default use a merge commit,
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
	{
		name: "Reset git directory modification times"
		run:  "touch -t 202211302355 $(find * -type d)"
	},
	{
		name: "Restore git file modification times"
		uses: "chetan/git-restore-mtime-action@075f9bc9d159805603419d50f794bd9f33252ebe"
	},

	{
		name: "Try to extract \(dispatchTrailer)"
		id:   dispatchTrailerStepID
		run:  """
			x="$(git log -1 --pretty='%(trailers:key=\(dispatchTrailer),valueonly)')"
			if [[ "$x" == "" ]]
			then
			   # Some steps rely on the presence or otherwise of the Dispatch-Trailer.
			   # We know that we don't have a Dispatch-Trailer in this situation,
			   # hence we use the JSON value null in order to represent that state.
			   # This means that GitHub expressions can determine whether a Dispatch-Trailer
			   # is present or not by checking whether the fromJSON() result of the
			   # output from this step is the JSON value null or not.
			   x=null
			fi
			echo "\(_dispatchTrailerDecodeStepOutputVar)<<EOD" >> $GITHUB_OUTPUT
			echo "$x" >> $GITHUB_OUTPUT
			echo "EOD" >> $GITHUB_OUTPUT
			"""
	},

	// Safety nets to flag if we ever have a Dispatch-Trailer slip through the
	// net and make it to master
	{
		name: "Check we don't have \(dispatchTrailer) on a protected branch"
		if:   "\(isProtectedBranch) && \(containsDispatchTrailer)"
		run:  """
			echo "\(_dispatchTrailerVariable) contains \(dispatchTrailer) but we are on a protected branch"
			false
			"""
	},
]

earlyChecks: githubactions.#Step & {
	name: "Early git and code sanity checks"
	run:  *"go run cuelang.org/go/internal/ci/checks@v0.13.2" | string
}

curlGitHubAPI: {
	#tokenSecretsKey: *botGitHubUserTokenSecretsKey | string

	#"""
	curl -s -L -H "Accept: application/vnd.github+json" -H "Authorization: Bearer ${{ secrets.\#(#tokenSecretsKey) }}" -H "X-GitHub-Api-Version: 2022-11-28"
	"""#
}

// setupCaches sets up a cache volume for the rest of the job.
// Our runner profiles on Namespace are already configured to only update
// the cache when they run from one of the protected branches.
//
// We cache for Go (GOCACHE and GOMODCACHE) by default, as most repos use it.
// These default caches are harmless for repos not using Go.
//
// Note that `${NSC_CACHE_PATH}` (`/cache`) is always mounted as a cache volume.
setupCaches: {
	#in: {
		additionalCaches: [...string] // with.cache
		additionalCachePaths: [...string] // with.path
	}

	[
		githubactions.#Step & {
			// We skip the cache entirely on the nightly runs, to catch flakes.
			// Note that this conditional is just a no-op for jobs without a nightly schedule.
			// TODO(mvdan): remove the windowsMachine condition once Windows supports caching on Namespace.
			if:   "github.event_name != 'schedule' && \(matrixRunnerExpr) != '\(windowsMachine)'"
			uses: "namespacelabs/nscloud-cache-action@v1"
			with: {
				let cacheModes = list.Concat([[
					"go",
				], #in.additionalCaches])
				let cachePaths = list.Concat([[
					// nothing here for now.
				], #in.additionalCachePaths])

				if len(cacheModes) > 0 {
					cache: strings.Join(cacheModes, "\n")
				}
				if len(cachePaths) > 0 {
					path: strings.Join(cachePaths, "\n")
				}
			}
		},

		// All tests on protected branches should skip the test cache,
		// which helps spot test flakes and bugs hidden by the caching.
		//
		// Critically, we don't skip the test cache on the trybot repo,
		// so that the testing of CLs can rely on an up to date test cache.
		githubactions.#Step & {
			if:  "github.repository == '\(githubRepositoryPath)' && (\(isProtectedBranch) || \(isTestDefaultBranch))"
			run: "go env -w GOFLAGS=-count=1"
		},
	]
}

// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
goChecks: githubactions.#Step & {
	run: """
		go mod tidy -diff
		go vet ./...
		"""
}

staticcheck: githubactions.#Step & {
	#in: modfile: string | *"" // an optional -modfile flag to not use the main go.mod
	let gotool = [
		if #in.modfile != "" {
			"go tool -modfile=\(#in.modfile)"
		},
		"go tool",
	][0]

	// TODO(mvdan): swap "/cache" for "${{ env.NSC_CACHE_PATH }}" once Namespace wires up that env var
	// for the workspace environment. See: https://discord.com/channels/975088590705012777/1397128547797176340
	env: STATICCHECK_CACHE: "/cache/staticcheck" // persist its cache
	run: "\(gotool) staticcheck ./..."
}

// isProtectedBranch is an expression that evaluates to true if the
// job is running as a result of pushing to one of protectedBranchPatterns.
// It would be nice to use the "contains" builtin for simplicity,
// but array literals are not yet supported in expressions.
isProtectedBranch: {
	#trailers: [...string]
	"((" + strings.Join([for branch in protectedBranchPatterns {
		(_matchPattern & {variable: "github.ref", pattern: "refs/heads/\(branch)"}).expr
	}], " || ") + ") && (! \(containsDispatchTrailer)))"
}

// isTestDefaultBranch is an expression that evaluates to true if
// the job is running on the testDefaultBranch
isTestDefaultBranch: "(github.ref == 'refs/heads/\(testDefaultBranch)')"

// #isReleaseTag creates a GitHub expression, based on the given release tag
// pattern, that evaluates to true if called in the context of a workflow that
// is part of a release.
isReleaseTag: {
	(_matchPattern & {variable: "github.ref", pattern: "refs/tags/\(releaseTagPattern)"}).expr
}

checkGitClean: githubactions.#Step & {
	name: "Check that git is clean at the end of the job"
	if:   "always()"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

repositoryDispatch: githubactions.#Step & {
	#githubRepositoryPath:         *githubRepositoryPath | string
	#botGitHubUserTokenSecretsKey: *botGitHubUserTokenSecretsKey | string
	#arg:                          _

	_curlGitHubAPI: curlGitHubAPI & {#tokenSecretsKey: #botGitHubUserTokenSecretsKey, _}

	name: string
	run:  #"""
			\#(_curlGitHubAPI) --fail --request POST --data-binary \#(strconv.Quote(json.Marshal(#arg))) https://api.github.com/repos/\#(#githubRepositoryPath)/dispatches
			"""#
}

workflowDispatch: githubactions.#Step & {
	#githubRepositoryPath:         *githubRepositoryPath | string
	#botGitHubUserTokenSecretsKey: *botGitHubUserTokenSecretsKey | string
	#workflowID:                   string

	// params are defined per https://docs.github.com/en/rest/actions/workflows?apiVersion=2022-11-28#create-a-workflow-dispatch-event
	#params: *{
		ref: defaultBranch
	} | _

	_curlGitHubAPI: curlGitHubAPI & {#tokenSecretsKey: #botGitHubUserTokenSecretsKey, _}

	name: string
	run:  #"""
			\#(_curlGitHubAPI) --fail --request POST --data-binary \#(strconv.Quote(json.Marshal(#params))) https://api.github.com/repos/\#(#githubRepositoryPath)/actions/workflows/\#(#workflowID)/dispatches
			"""#
}

// dispatchTrailer is the trailer that we use to pass information in a commit
// when triggering workflow events in other GitHub repos.
//
// NOTE: keep this consistent with gerritstatusupdater parsing logic.
dispatchTrailer: "Dispatch-Trailer"

// dispatchTrailerStepID is the ID of the step that attempts
// to extract a Dispatch-Trailer value from the commit at HEAD
dispatchTrailerStepID: strings.Replace(dispatchTrailer, "-", "", -1)

// _dispatchTrailerDecodeStepOutputVar is the name of the output
// variable int he dispatchTrailerStepID step
_dispatchTrailerDecodeStepOutputVar: "value"

// dispatchTrailerExpr is a GitHub expression that can be dereferenced
// to get values from the JSON-decded Dispatch-Trailer value that
// is extracted during the dispatchTrailerStepID step.
dispatchTrailerExpr: "fromJSON(steps.\(dispatchTrailerStepID).outputs.\(_dispatchTrailerDecodeStepOutputVar))"

// containsDispatchTrailer returns a GitHub expression that looks at the commit
// message of the head commit of the event that triggered the workflow, an
// expression that returns true if the commit message associated with that head
// commit contains dispatchTrailer.
//
// Note that this logic does not 100% match the answer that would be returned by:
//
//      git log --pretty=%(trailers:key=Dispatch-Trailer,valueonly)
//
// GitHub expressions are incredibly limited in their capabilities:
//
//     https://docs.github.com/en/actions/learn-github-actions/expressions
//
// There is not even a regular expression matcher. Hence the logic is a best-efforts
// approximation of the logic employed by git log.
containsDispatchTrailer: {
	#type?: string

	// If we have a value for #type, then match against that value.
	// Otherwise the best we can do is match against:
	//
	//     Dispatch-Trailer: {"type:}
	//
	let _typeCheck = [if #type != _|_ {#type + "\""}, ""][0]
	"""
	(contains(\(_dispatchTrailerVariable), '\n\(dispatchTrailer): {"type":"\(_typeCheck)'))
	"""
}

containsTrybotTrailer: containsDispatchTrailer & {
	#type: trybot.key
	_
}

containsUnityTrailer: containsDispatchTrailer & {
	#type: unity.key
	_
}

_dispatchTrailerVariable: "github.event.head_commit.message"

loginCentralRegistry: githubactions.#Step & {
	#cueCommand:      *cueCommand | string
	#tokenExpression: *"${{ secrets.\(unprivilegedBotGitHubUserCentralRegistryTokenSecretsKey) }}" | string
	run:              "\(#cueCommand) login --token=\(#tokenExpression)"
}
