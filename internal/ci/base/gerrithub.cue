package base

// This file contains gerrithub related definitions etc

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#dispatchWorkflow: json.#Workflow & {
	#type:                  string
	_#branchNameExpression: "\(#type)/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                   "Dispatch \(#type)"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		(#type): {
			"runs-on": params.linuxMachine
			if:        "${{ github.event.client_payload.type == '\(#type)' }}"
			steps: [
				#writeNetrcFile,
				// Out of the entire ref (e.g. refs/changes/38/547738/7) we only
				// care about the CL number and patchset, (e.g. 547738/7).
				// Note that gerrithub_ref is two path elements.
				json.#step & {
					id: "gerrithub_ref"
					run: #"""
						ref="$(echo ${{github.event.client_payload.payload.ref}} | sed -E 's/^refs\/changes\/[0-9]+\/([0-9]+)\/([0-9]+).*/\1\/\2/')"
						echo "gerrithub_ref=$ref" >> $GITHUB_OUTPUT
						"""#
				},
				json.#step & {
					name: "Trigger \(#type)"
					run:  """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(params.botGitHubUser)
						git config user.email \(params.botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(params.botGitHubUser):${{ secrets.\(params.botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(params.gerritHubRepository) "${{ github.event.client_payload.payload.ref }}"
						git checkout -b \(_#branchNameExpression) FETCH_HEAD
						git remote add origin \(params.trybotRepositoryURL)
						git fetch origin "${{ github.event.client_payload.payload.branch }}"
						git push origin \(_#branchNameExpression)
						echo ${{ secrets.CUECKOO_GITHUB_PAT }} | gh auth login --with-token
						gh pr --repo=\(params.trybotRepositoryURL) create --base="${{ github.event.client_payload.payload.branch }}" --fill
						"""
				},
			]
		}
	}
}

#pushTipToTrybotWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"

	name: "Push tip to \(#trybot.key)"

	concurrency: "push_tip_to_trybot"

	jobs: push: {
		steps: [
			#writeNetrcFile,
			json.#step & {
				name: "Push tip to trybot"
				run:  """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(params.botGitHubUser)
						git config user.email \(params.botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(params.botGitHubUser):${{ secrets.\(params.botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add origin \(params.gerritHubRepository)
						git remote add trybot \(params.trybotRepositoryURL)
						git fetch origin "${{ github.ref }}"
						git push trybot "FETCH_HEAD:${{ github.ref }}"
						"""
			},
		]
	}

}

#writeNetrcFile: json.#step & {
	name: "Write netrc file for cueckoo Gerrithub"
	run:  """
			cat <<EOD > ~/.netrc
			machine \(params.gerritHubHostname)
			login \(params.botGerritHubUser)
			password ${{ secrets.\(params.botGerritHubUserPasswordSecretsKey) }}
			EOD
			chmod 600 ~/.netrc
			"""
}
