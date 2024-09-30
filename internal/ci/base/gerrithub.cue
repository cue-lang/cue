package base

// This file contains gerrithub related definitions etc

import (
	"encoding/json"
	"strings"

	"github.com/cue-tmp/jsonschema-pub/exp1/githubactions"
)

// trybotWorkflows is a template for trybot-based repos
trybotWorkflows: {
	(trybot.key): githubactions.#Workflow & {
		on: workflow_dispatch: {}
	}
	"\(trybot.key)_dispatch":    trybotDispatchWorkflow
	"push_tip_to_\(trybot.key)": pushTipToTrybotWorkflow
	"evict_caches":              evictCaches
}

#dispatch: {
	type:         string
	CL:           int
	patchset:     int
	targetBranch: *defaultBranch | string

	let p = strings.Split("\(CL)", "")
	let rightMostTwo = p[len(p)-2] + p[len(p)-1]
	ref: *"refs/changes/\(rightMostTwo)/\(CL)/\(patchset)" | string
}

trybotDispatchWorkflow: bashWorkflow & {
	#dummyDispatch?: #dispatch
	name:            "Dispatch \(trybot.key)"
	on: {
		repository_dispatch: {}
		push: {
			// To enable testing of the dispatch itself
			branches: [testDefaultBranch]
		}
	}
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		(trybot.key): {
			"runs-on": linuxMachine

			let goodDummyData = [if json.Marshal(#dummyDispatch) != _|_ {true}, false][0]

			// We set the "on" conditions above, but this would otherwise mean we
			// run for all dispatch events.
			if: "${{ (\(isTestDefaultBranch) && \(goodDummyData)) || github.event.client_payload.type == '\(trybot.key)' }}"

			// See the comment below about the need for cases
			let cases = [
				{
					condition:  "!="
					expr:       "fromJSON(steps.payload.outputs.value)"
					nameSuffix: "fake data"
				},
				{
					condition:  "=="
					expr:       "github.event.client_payload"
					nameSuffix: "repository_dispatch payload"
				},
			]

			steps: [
				writeNetrcFile,

				githubactions.#Step & {
					name: "Write fake payload"
					id:   "payload"
					if:   "github.repository == '\(githubRepositoryPath)' && \(isTestDefaultBranch)"

					// Use bash heredocs so that JSON's use of double quotes does
					// not get interpreted as shell.  Both in the running of the
					// command itself, which itself is the echo-ing of a command to
					// $GITHUB_OUTPUT.
					run: #"""
						cat <<EOD >> $GITHUB_OUTPUT
						value<<DOE
						\#(*json.Marshal(#dummyDispatch) | "null")
						DOE
						EOD
						"""#
				},

				// GitHub does not allow steps with the same ID, even if (by virtue
				// of runtime 'if' expressions) both would not actually run. So
				// we have to duplciate the steps that follow with those same
				// runtime expressions
				//
				// Hence we have to create two steps, one to trigger if the
				// repository_dispatch payload is set, and one if not (i.e. we use
				// the fake payload).
				for v in cases {
					let localBranchExpr = "local_${{ \(v.expr).targetBranch }}"
					let targetBranchExpr = "${{ \(v.expr).targetBranch }}"
					githubactions.#Step & {
						name: "Trigger \(trybot.name) (\(v.nameSuffix))"
						if:   "github.event.client_payload.type \(v.condition) '\(trybot.key)'"
						run:  """
						mkdir tmpgit
						cd tmpgit
						git init -b initialbranch
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add origin  \(gerritHubRepositoryURL)

						git fetch origin ${{ \(v.expr).ref }}
						git checkout -b \(localBranchExpr) FETCH_HEAD

						# Error if we already have dispatchTrailer according to git log logic.
						# See earlier check for GitHub expression logic check.
						x="$(git log -1 --pretty='%(trailers:key=\(dispatchTrailer),valueonly)')"
						if [[ "$x" != "" ]]
						then
							 echo "Ref ${{ \(v.expr).ref }} already has a \(dispatchTrailer)"
							 exit 1
						fi

						# Add the trailer because we don't have it yet. GitHub expressions do not have a
						# substitute or quote capability. So we do that in shell. We also strip out the
						# indenting added by toJSON. We ensure that the type field is first in order
						# that we can safely check for specific types of dispatch trailer.
						#
						# Use bash heredoc so that JSON's use of double quotes does
						# not get interpreted as shell.
						trailer="$(cat <<EOD | jq -r -c '{type} + .'
						${{ toJSON(\(v.expr)) }}
						EOD
						)"
						git log -1 --format=%B | git interpret-trailers --trailer "\(dispatchTrailer): $trailer" | git commit --amend -F -
						git log -1

						success=false
						for try in {1..20}; do
							echo "Push to trybot try $try"
							if git push -f \(trybotRepositoryURL) \(localBranchExpr):\(targetBranchExpr); then
								success=true
								break
							fi
							sleep 1
						done
						if ! $success; then
							echo "Giving up"
							exit 1
						fi
						"""
					}
				},
			]
		}
	}
}

pushTipToTrybotWorkflow: bashWorkflow & {
	jobs: [string]: defaults: run: shell: "bash"

	on: {
		push: branches: protectedBranchPatterns
	}
	jobs: push: {
		"runs-on": linuxMachine
		if:        "${{github.repository == '\(githubRepositoryPath)'}}"
	}

	name: "Push tip to \(trybot.key)"

	concurrency: "push_tip_to_trybot"

	jobs: push: {
		steps: [
			writeNetrcFile,
			githubactions.#Step & {
				name: "Push tip to trybot"
				run:  """
						mkdir tmpgit
						cd tmpgit
						git init -b initialbranch
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add origin \(gerritHubRepositoryURL)
						git remote add trybot \(trybotRepositoryURL)

						git fetch origin "${{ github.ref }}"

						success=false
						for try in {1..20}; do
							 echo "Push to trybot try $try"
							 if git push -f trybot "FETCH_HEAD:${{ github.ref }}"; then
								  success=true
								  break
							 fi
							 sleep 1
						done
						if ! $success; then
							 echo "Giving up"
							 exit 1
						fi
						"""
			},
		]
	}

}

// evictCaches removes "old" GitHub actions caches from the main repo and the
// accompanying trybot  The job is only run in the main repo, because
// that is the only place where the credentials exist.
//
// The GitHub actions caches in the main and trybot repos can get large. So
// large in fact we got the following warning from GitHub:
//
//   "Approaching total cache storage limit (34.5 GB of 10 GB Used)"
//
// Yes, you did read that right.
//
// Not only does this have the effect of causing us to breach "limits" it also
// means that we can't be sure that individual caches are not bloated.
//
// Fix that by purging the actions caches on a daily basis at 0200, followed 15
// mins later by a re-run of the tip trybots to repopulate the caches so they
// are warm and minimal.
//
// In testing with @mvdan, this resulted in cache sizes for Linux dropping from
// ~1GB to ~125MB. This is a considerable saving.
//
// Note this currently removes all cache entries, regardless of whether they
// are go-related or not. We should revisit this later.
evictCaches: bashWorkflow & {
	name: "Evict caches"

	on: {
		schedule: [
			{cron: "0 2 * * *"},
		]
	}

	jobs: {
		test: {
			// We only want to run this in the main repo
			if:        "${{github.repository == '\(githubRepositoryPath)'}}"
			"runs-on": linuxMachine
			steps: [
				for v in checkoutCode {v},

				githubactions.#Step & {
					name: "Delete caches"
					run:  """
						set -x

						echo ${{ secrets.\(botGitHubUserTokenSecretsKey) }} | gh auth login --with-token
						gh extension install actions/gh-actions-cache
						for i in \(githubRepositoryURL) \(trybotRepositoryURL)
						do
							echo "Evicting caches for $i"
							cd $(mktemp -d)
							git init -b initialbranch
							git remote add origin $i
							for j in $(gh actions-cache list -L 100 | grep refs/ | awk '{print $1}')
							do
							   gh actions-cache delete --confirm $j
							done
						done
						"""
				},

				githubactions.#Step & {
					name: "Trigger workflow runs to repopulate caches"
					let branchPatterns = strings.Join(protectedBranchPatterns, " ")

					run: """
						# Prepare git for pushes to trybot repo. Note
						# because we have already checked out code we don't
						# need origin. Fetch origin default branch for later use
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add trybot \(trybotRepositoryURL)

						# Now trigger the most recent workflow run on each of the default branches.
						# We do this by listing all the branches on the main repo and finding those
						# which match the protected branch patterns (globs).
						for j in $(\(curlGitHubAPI) -f https://api.github.com/repos/\(githubRepositoryPath)/branches | jq -r '.[] | .name')
						do
							for i in \(branchPatterns)
							do
								if [[ "$j" != $i ]]; then
									continue
								fi

								echo Branch: $j
								sha=$(\(curlGitHubAPI) "https://api.github.com/repos/\(githubRepositoryPath)/commits/$j" | jq -r '.sha')
								echo Latest commit: $sha

								echo "Trigger workflow on \(githubRepositoryPath)"
								\(curlGitHubAPI) --fail-with-body -X POST https://api.github.com/repos/\(githubRepositoryPath)/actions/workflows/\(trybot.key+workflowFileExtension)/dispatches -d "{\\"ref\\":\\"$j\\"}"

								# Ensure that the trybot repo has the latest commit for
								# this branch.  If the force-push results in a commit
								# being pushed, that will trigger the trybot workflows
								# so we don't need to do anything, otherwise we need to
								# trigger the most recent commit on that branch
								git remote -v
								git fetch origin refs/heads/$j
								git log -1 FETCH_HEAD

								success=false
								for try in {1..20}; do
									echo "Push to trybot try $try"
									exitCode=0; push="$(git push -f trybot FETCH_HEAD:$j 2>&1)" || exitCode=$?
									echo "$push"
									if [[ $exitCode -eq 0 ]]; then
										success=true
										break
									fi
									sleep 1
								done
								if ! $success; then
									echo "Giving up"
									exit 1
								fi

								if echo "$push" | grep up-to-date
								then
									# We are up-to-date, i.e. the push did nothing, hence we need to trigger a workflow_dispatch
									# in the trybot repo.
									echo "Trigger workflow on \(trybotRepositoryPath)"
									\(curlGitHubAPI) --fail-with-body -X POST https://api.github.com/repos/\(trybotRepositoryPath)/actions/workflows/\(trybot.key+workflowFileExtension)/dispatches -d "{\\"ref\\":\\"$j\\"}"
								else
									echo "Force-push to \(trybotRepositoryPath) did work; nothing to do"
								fi
							done
						done
						"""
				},
			]
		}
	}
}

writeNetrcFile: githubactions.#Step & {
	name: "Write netrc file for \(botGerritHubUser) Gerrithub"
	run:  """
			cat <<EOD > ~/.netrc
			machine \(gerritHubHostname)
			login \(botGerritHubUser)
			password ${{ secrets.\(botGerritHubUserPasswordSecretsKey) }}
			EOD
			chmod 600 ~/.netrc
			"""
}
