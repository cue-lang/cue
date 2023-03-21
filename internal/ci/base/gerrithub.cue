package base

// This file contains gerrithub related definitions etc

import (
	encjson "encoding/json"
	"strings"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// trybotWorkflows is a template for trybot-based repos
trybotWorkflows: {
	(trybot.key):                json.#Workflow
	"\(trybot.key)_dispatch":    trybotDispatchWorkflow
	"push_tip_to_\(trybot.key)": pushTipToTrybotWorkflow
	"evict_caches":              evictCaches
}

#dispatch: {
	type:         string
	CL:           int
	patchset:     int
	targetBranch: *defaultBranch | string
	ref:          *"refs/changes/\(mod(CL, 100))/\(CL)/\(patchset)" | string
}

trybotDispatchWorkflow: bashWorkflow & {
	#dummyDispatch: #dispatch
	name:           "Dispatch \(trybot.key)"
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

			// We set the "on" conditions above, but this would otherwise mean we
			// run for all dispatch events.
			if: "${{ github.ref == 'refs/heads/\(testDefaultBranch)' || github.event.client_payload.type == '\(trybot.key)' }}"

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

				json.#step & {
					name: "Write fake payload"
					id:   "payload"
					if:   "github.repository == '\(githubRepositoryPath)' && github.ref == 'refs/heads/\(testDefaultBranch)'"
					run:  #"""
						cat <<EOD >> $GITHUB_OUTPUT
						value<<DOE
						\#(encjson.Marshal(#dummyDispatch))
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
					json.#step & {
						name: "Trigger \(trybot.name) (\(v.nameSuffix))"
						if:   "github.event.client_payload.type \(v.condition) '\(trybot.key)'"
						run:  """
						set -x

						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add origin  \(gerritHubRepositoryURL)

						# We also (temporarily) get the default branch in order that
						# we can "restore" the trybot repo to a good state for the
						# current (i.e. previous) implementation of trybots which
						# used PRs. If the target branch in the trybot repo is not
						# current, then PR creation will fail because GitHub claims
						# it cannot find any link between the commit in a PR (i.e.
						# the CL under test in the previous setup) and the target
						# branch which, under the new setup, might well currently
						# be the commit from a CL.
						git fetch origin ${{ \(v.expr).targetBranch }}

						git fetch origin ${{ \(v.expr).ref }}
						git checkout -b ${{ \(v.expr).targetBranch }} FETCH_HEAD

						# Error if we already have dispatchTrailer according to git log logic.
						# See earlier check for GitHub expression logic check.
						x="$(git log -1 --pretty='%(trailers:key=\(dispatchTrailer),valueonly)')"
						if [ "$x" != "" ]
						then
							 echo "Ref ${{ \(v.expr).ref }} already has a \(dispatchTrailer)"
							 exit 1
						fi

						# Add the trailer because we don't have it yet. GitHub expressions do not have a
						# substitute or quote capability. So we do that in shell. We also strip out the
						# indenting added by toJSON. We ensure that the type field is first in order
						# that we can safely check for specific types of dispatch trailer.
						trailer="$(cat <<EOD | jq -c '{type} + .'
						${{ toJSON(\(v.expr)) }}
						EOD
						)"
						git log -1 --format=%B | git interpret-trailers --trailer "\(dispatchTrailer): $trailer" | git commit --amend -F -
						git log -1

						success=false
						for try in {1..20}; do
							echo "Push to trybot try $try"
							if git push -f \(trybotRepositoryURL) ${{ \(v.expr).targetBranch }}:${{ \(v.expr).targetBranch }}; then
								success=true
								break
							fi
							sleep 1
						done
						if ! $success; then
							echo "Giving up"
							exit 1
						fi

						# Restore the default branch on the trybot repo to be the tip of the main repo
						success=false
						for try in {1..20}; do
							echo "Push to trybot try $try"
							if git push -f \(trybotRepositoryURL) origin/${{ \(v.expr).targetBranch }}:${{ \(v.expr).targetBranch }}; then
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
			json.#step & {
				name: "Push tip to trybot"
				run:  """
						mkdir tmpgit
						cd tmpgit
						git init
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
				json.#step & {
					let branchPatterns = strings.Join(protectedBranchPatterns, " ")

					// rerunLatestWorkflow runs the latest trybot workflow in the
					// specified repo for branches that match the specified branch.
					let rerunLatestWorkflow = {
						#repo:   string
						#branch: string
						"""
						id=$(\(curlGitHubAPI) "https://api.github.com/repos/\(#repo)/actions/workflows/\(trybot.key).yml/runs?branch=\(#branch)&event=push&per_page=1" | jq '.workflow_runs[] | .id')
						\(curlGitHubAPI) -X POST https://api.github.com/repos/\(#repo)/actions/runs/$id/rerun

						"""
					}

					run: """
						set -eux

						echo ${{ secrets.\(botGitHubUserTokenSecretsKey) }} | gh auth login --with-token
						gh extension install actions/gh-actions-cache
						for i in \(githubRepositoryURL) \(trybotRepositoryURL)
						do
							echo "Evicting caches for $i"
							cd $(mktemp -d)
							git init
							git remote add origin $i
							for j in $(gh actions-cache list -L 100 | grep refs/ | awk '{print $1}')
							do
								gh actions-cache delete --confirm $j
							done
						done

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

								echo "$j is a match with $i"
								\(rerunLatestWorkflow & {#repo: githubRepositoryPath, #branch: "$j", _})
								\(rerunLatestWorkflow & {#repo: trybotRepositoryPath, #branch: "$j", _})
							done
						done
						"""
				},
			]
		}
	}
}

writeNetrcFile: json.#step & {
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
