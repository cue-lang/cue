package base

// This file contains gerrithub related definitions etc

import (
	encjson "encoding/json"
	"strings"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#dispatch: {
	type:         string
	CL:           int
	patchset:     int
	targetBranch: string
	ref:          string
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

			steps: [
				writeNetrcFile,

				json.#step & {
					id:  "payload"
					if:  "github.repository == '\(githubRepositoryPath)' && github.ref == 'refs/heads/\(testDefaultBranch)'"
					run: #"""
						cat <<EOD >> $GITHUB_OUTPUT
						value<<DOE
						\#(encjson.Marshal(#dummyDispatch))
						DOE
						EOD
						"""#
				},

				// Assert according to the containsSpecialTrailers expression that we do not
				// contain any special trailers. Note this appears to duplicate the check below
				// but verifying against the GitHub expression logic here is critical to ensuring
				// we catch any discrepancies in the matching logic
				json.#step & {
					if: containsSpecialTrailers
					run: """
						echo "contains special trailers according to containsSpecialTrailers" && false
						"""
				},

				// GitHub does not allow steps with the same ID, even if (by virtue of
				// runtime 'if' expressions) both would not actually run. So we have
				// to duplciate the steps that follow with those same runtime expressions
				for v in [{condition: "!=", expr: "fromJSON(steps.payload.outputs.value)"}, {condition: "==", expr: "github.event.client_payload"}] {
					json.#step & {
						let gitSpecialTrailerChecks = [ for trailer in specialTrailers {
							"""
							# Error if we already have \(trailer) according to git log logic.
							# See earlier check for GitHub expression logic check.
							x="$(git log -1 --pretty='%(trailers:key=\(trailer),valueonly)')"
							if [ "$x" != "" ]
							then
							    echo "Ref ${{ \(v.expr).ref }} already has a \(trailer)"
							    exit 1
							fi
							"""
						}]

						name: "Trigger \(trybot.name)"
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

						\(strings.Join(gitSpecialTrailerChecks, "\n"))

						# Add the trailer because we don't have it yet. GitHub expressions do not have a
						# substitute or quote capability. So we do that in shell. We also strip out the
						# indenting added by toJSON.
						trailer="$(cat <<EOD | jq -r --indent 0
						${{ toJSON(\(v.expr)) }}
						EOD
						)"
						git log -1 --format=%B | git interpret-trailers --trailer "\(trybot.trailer): $trailer" | git commit --amend -F -
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

writeNetrcFile: json.#step & {
	name: "Write netrc file for cueckoo Gerrithub"
	run:  """
			cat <<EOD > ~/.netrc
			machine \(gerritHubHostname)
			login \(botGerritHubUser)
			password ${{ secrets.\(botGerritHubUserPasswordSecretsKey) }}
			EOD
			chmod 600 ~/.netrc
			"""
}
