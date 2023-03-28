package base

// This file contains gerrithub related definitions etc

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

trybotDispatchWorkflow: bashWorkflow & {
	_#branchNameExpression: "\(trybot.key)/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                   "Dispatch \(trybot.key)"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		(trybot.key): {
			"runs-on": linuxMachine
			if:        "${{ github.event.client_payload.type == '\(trybot.key)' }}"
			steps: [
				writeNetrcFile,
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
					name: "Trigger \(trybot.key)"
					run:  """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(botGitHubUser)
						git config user.email \(botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(botGitHubUser):${{ secrets.\(botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(gerritHubRepositoryURL) "${{ github.event.client_payload.payload.ref }}"
						git checkout -b \(_#branchNameExpression) FETCH_HEAD
						git remote add origin \(trybotRepositoryURL)
						git fetch origin "${{ github.event.client_payload.payload.branch }}"
						git push origin \(_#branchNameExpression)
						echo ${{ secrets.CUECKOO_GITHUB_PAT }} | gh auth login --with-token
						gh pr --repo=\(trybotRepositoryURL) create --base="${{ github.event.client_payload.payload.branch }}" --fill
						"""
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
