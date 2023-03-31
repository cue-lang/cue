package base

// This file contains gerrithub related definitions etc

import (
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
						echo ${{ secrets.\(botGitHubUserTokenSecretsKey) }} | gh auth login --with-token
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
