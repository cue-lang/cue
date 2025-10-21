package base

// This file contains gerrithub related definitions etc

import (
	"encoding/json"
	"strings"

	"cue.dev/x/githubactions"
)

// trybotWorkflows is a template for trybot-based repos
trybotWorkflows: {
	(trybot.key): githubactions.#Workflow & {
		name: trybot.name
		on: {
			// Run nightly at 2am UTC without a cache to catch flakes.
			schedule: [{cron: "0 2 * * *"}]
			// Triggering a trybot job via a workflow_dispatch can be a useful way
			// to manually or automatically start a job without needing to git push.
			workflow_dispatch: {}
		}
	}
	"\(trybot.key)_dispatch":    trybotDispatchWorkflow
	"push_tip_to_\(trybot.key)": pushTipToTrybotWorkflow
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
	jobs: {
		(trybot.key): {
			"runs-on": linuxSmallMachine + overrideCacheTagDispatch

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

				{
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
					{
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
						# --no-divider prevents a "---" line from marking the end of the commit message.
						# Here, we know that the input is exactly one commit message,
						# so don't do weird things if the commit message has a "---" line.
						git log -1 --format=%B | git interpret-trailers --no-divider --trailer "\(dispatchTrailer): $trailer" | git commit --amend -F -
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
	on: {
		push: branches: protectedBranchPatterns
	}
	name:        "Push tip to \(trybot.key)"
	concurrency: "push_tip_to_trybot"

	jobs: push: {
		"runs-on": linuxSmallMachine + overrideCacheTagDispatch
		if:        "${{github.repository == '\(githubRepositoryPath)'}}"
		steps: [
			writeNetrcFile,
			{
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
