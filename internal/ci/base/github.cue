package base

// This file contains aspects principally related to GitHub workflows

import (
	"encoding/json"
	"list"
	"strings"
	"strconv"

	"github.com/cue-tmp/jsonschema-pub/exp1/githubactions"
)

bashWorkflow: githubactions.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

installGo: {
	#setupGo: githubactions.#Step & {
		name: "Install Go"
		uses: "actions/setup-go@v5"
		with: {
			// We do our own caching in setupGoActionsCaches.
			cache:        false
			"go-version": string
		}
	}

	// Why set GOTOOLCHAIN here? As opposed to an environment variable
	// elsewhere? No perfect answer to this question but here is the thinking:
	//
	// Setting the variable here localises it with the installation of Go. Doing
	// it elsewhere creates distance between the two steps which are
	// intrinsically related. And it's also hard to do: "when we use this step,
	// also ensure that we establish an environment variable in the job for
	// GOTOOLCHAIN".
	//
	// Environment variables can only be set at a workflow, job or step level.
	// Given we currently use a matrix strategy which varies the Go version,
	// that rules out using an environment variable based approach, because the
	// Go version is only available at runtime via GitHub actions provided
	// context. Whether we should instead be templating multiple workflows (i.e.
	// exploding the matrix ourselves) is a different question, but one that
	// has performance implications.
	//
	// So as clumsy as it is to use a step "template" that includes more than
	// one step, it's the best option available to us for now.
	[
		#setupGo,

		{
			githubactions.#Step & {
				name: "Set common go env vars"
				run: """
					go env -w GOTOOLCHAIN=local

					# Dump env for good measure
					go env
					"""
			}
		},
	]
}

checkoutCode: {
	#actionsCheckout: githubactions.#Step & {
		name: "Checkout code"
		uses: "actions/checkout@v4"

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
		githubactions.#Step & {
			name: "Reset git directory modification times"
			run:  "touch -t 202211302355 $(find * -type d)"
		},
		githubactions.#Step & {
			name: "Restore git file modification times"
			uses: "chetan/git-restore-mtime-action@075f9bc9d159805603419d50f794bd9f33252ebe"
		},

		{
			githubactions.#Step & {
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
			}
		},

		// Safety nets to flag if we ever have a Dispatch-Trailer slip through the
		// net and make it to master
		githubactions.#Step & {
			name: "Check we don't have \(dispatchTrailer) on a protected branch"
			if:   "\(isProtectedBranch) && \(containsDispatchTrailer)"
			run:  """
				echo "\(_dispatchTrailerVariable) contains \(dispatchTrailer) but we are on a protected branch"
				false
				"""
		},
	]
}

earlyChecks: githubactions.#Step & {
	name: "Early git and code sanity checks"
	run:  *"go run cuelang.org/go/internal/ci/checks@v0.11.0-0.dev.0.20240903133435-46fb300df650" | string
}

curlGitHubAPI: {
	#tokenSecretsKey: *botGitHubUserTokenSecretsKey | string

	#"""
	curl -s -L -H "Accept: application/vnd.github+json" -H "Authorization: Bearer ${{ secrets.\#(#tokenSecretsKey) }}" -H "X-GitHub-Api-Version: 2022-11-28"
	"""#
}

setupGoActionsCaches: {
	// #readonly determines whether we ever want to write the cache back. The
	// writing of a cache back (for any given cache key) should only happen on a
	// protected branch. But running a workflow on a protected branch often
	// implies that we want to skip the cache to ensure we catch flakes early.
	// Hence the concept of clearing the testcache to ensure we catch flakes
	// early can be defaulted based on #readonly. In the general case the two
	// concepts are orthogonal, hence they are kept as two parameters, even
	// though in our case we could get away with a single parameter that
	// encapsulates our needs.
	#readonly:       *false | bool
	#cleanTestCache: *!#readonly | bool
	#goVersion:      string
	#additionalCacheDirs: [...string]
	#os: string

	let goModCacheDirID = "go-mod-cache-dir"
	let goCacheDirID = "go-cache-dir"

	// cacheDirs is a convenience variable that includes
	// GitHub expressions that represent the directories
	// that participate in Go caching.
	let cacheDirs = list.Concat([[
		"${{ steps.\(goModCacheDirID).outputs.dir }}/cache/download",
		"${{ steps.\(goCacheDirID).outputs.dir }}",
	], #additionalCacheDirs])

	let cacheRestoreKeys = "\(#os)-\(#goVersion)"

	let cacheStep = githubactions.#Step & {
		with: {
			path: strings.Join(cacheDirs, "\n")

			// GitHub actions caches are immutable. Therefore, use a key which is
			// unique, but allow the restore to fallback to the most recent cache.
			// The result is then saved under the new key which will benefit the
			// next build. Restore keys are only set if the step is restore.
			key:            "\(cacheRestoreKeys)-${{ github.run_id }}"
			"restore-keys": cacheRestoreKeys
		}
	}

	let readWriteCacheExpr = "(\(isProtectedBranch) || \(isTestDefaultBranch))"

	// pre is the list of steps required to establish and initialise the correct
	// caches for Go-based workflows.
	[
		// TODO: once https://github.com/actions/setup-go/issues/54 is fixed,
		// we could use `go env` outputs from the setup-go step.
		githubactions.#Step & {
			name: "Get go mod cache directory"
			id:   goModCacheDirID
			run:  #"echo "dir=$(go env GOMODCACHE)" >> ${GITHUB_OUTPUT}"#
		},
		githubactions.#Step & {
			name: "Get go build/test cache directory"
			id:   goCacheDirID
			run:  #"echo "dir=$(go env GOCACHE)" >> ${GITHUB_OUTPUT}"#
		},

		// Only if we are not running in readonly mode do we want a step that
		// uses actions/cache (read and write). Even then, the use of the write
		// step should be predicated on us running on a protected branch. Because
		// it's impossible for anything else to write such a cache.
		if !#readonly {
			cacheStep & {
				if:   readWriteCacheExpr
				uses: "actions/cache@v4"
			}
		},

		cacheStep & {
			// If we are readonly, there is no condition on when we run this step.
			// It should always be run, becase there is no alternative. But if we
			// are not readonly, then we need to predicate this step on us not
			// being on a protected branch.
			if !#readonly {
				if: "! \(readWriteCacheExpr)"
			}

			uses: "actions/cache/restore@v4"
		},

		if #cleanTestCache {
			// All tests on protected branches should skip the test cache.  The
			// canonical way to do this is with -count=1. However, we want the
			// resulting test cache to be valid and current so that subsequent CLs
			// in the trybot repo can leverage the updated cache. Therefore, we
			// instead perform a clean of the testcache.
			//
			// Critically we only want to do this in the main repo, not the trybot
			// repo.
			githubactions.#Step & {
				if:  "github.repository == '\(githubRepositoryPath)' && (\(isProtectedBranch) || github.ref == 'refs/heads/\(testDefaultBranch)')"
				run: "go clean -testcache"
			}
		},
	]
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
