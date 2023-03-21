package base

// This file contains gerrithub related definitions etc

import (
	encjson "encoding/json"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#dispatch: {
	type:         string
	CL:           int
	patchset:     int
	targetBranch: string
	ref:          string
}

dummyDispatch: #dispatch & {
	type:         trybot.key
	CL:           551352
	patchset:     _
	targetBranch: "master"
	ref:          "refs/changes/\(mod(CL, 100))/\(CL)/\(patchset)"
}

trybotDispatchWorkflow: json.#Workflow & {
	#type: string
	name:  "Dispatch \(#type)"
	on: {
		repository_dispatch: {}
		push: {
			// To enable testing of the dispatch itself
			branches: [testDefaultBranch]
		}
	}
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		(#type): {
			"runs-on": linuxMachine

			// We set the "on" conditions above, but this would otherwise mean we
			// run for all dispatch events.
			if: "${{ github.ref == 'refs/heads/\(testDefaultBranch)' || github.event.client_payload.type == '\(#type)' }}"

			steps: [
				writeNetrcFile,

				json.#step & {
					id:  "payload"
					if:  "github.repository == '\(githubRepositoryPath)' && github.ref == 'refs/heads/\(testDefaultBranch)'"
					run: #"""
						cat <<EOD >> $GITHUB_OUTPUT
						value<<DOE
						\#(encjson.Marshal(dummyDispatch))
						DOE
						EOD
						"""#
				},

				// GitHub does not allow steps with the same ID, even if (by virtue of
				// runtime 'if' expressions) both would not actually run. So we have
				// to duplciate the steps that follow with those same runtime expressions
				for v in [{condition: "!=", expr: "fromJSON(steps.payload.outputs.value)"}, {condition: "==", expr: "github.event.client_payload"}] {
					json.#step & {
						name: "Trigger \(#type)"
						if:   "github.event.client_payload.type \(v.condition) '\(#type)'"
						run:  """
						set -x

						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(gerritHubRepositoryURL) "${{ \(v.expr).ref }}"
						git checkout -b ${{ \(v.expr).targetBranch }} FETCH_HEAD

						# Error if we already have the trailer
						x="$(git log -1 --pretty='%(trailers:key=\(trybot.trailer),valueonly)')"
						if [ "$x" != "" ]
						then
							echo "Ref ${{ \(v.expr).ref }} already has a =\(trybot.trailer)"
							exit 1
						fi


						# Add the trailer because we don't have it yet. GitHub expressions do not have a
						# substitute or quote capability. So we do that in shell. We also strip out the
						# indenting added by toJSON.
						trailer="$(cat <<EOD | jq -r --indent 0
						${{ toJSON(\(v.expr)) }}
						EOD
						)"
						git log -1 --format=%B | git interpret-trailers --trailer "\(trybot.trailer): $trailer" | git commit --amend -F -
						git log -1

						git push -f \(trybotRepositoryURL) ${{ \(v.expr).targetBranch }}:${{ \(v.expr).targetBranch }}
						"""
					}
				},
			]
		}
	}
}

pushTipToTrybotWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"

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

						# Very rough exponential backoff of retries
						sleepInterval=1
						for try in {1..20}
						do
							echo Push to trybot try $try
							git push -f trybot "FETCH_HEAD:${{ github.ref }}" && break
							sleep $sleepInterval
							sleepInterval=$(( sleepInterval * 2 ))
						done
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
