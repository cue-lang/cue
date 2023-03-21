// Copyright 2023 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package github

import (
	"strings"

	"cuelang.org/go/internal/ci/core"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The evict_caches removes "old" GitHub actions caches from the main repo and
// the accompanying trybot repo. The job is only run in the main repo, because
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
workflows: evict_caches: _base.#bashWorkflow & {
	name: "Evict caches"

	on: {
		schedule: [
			{cron: "0 2 * * *"},
		]
	}

	jobs: {
		test: {
			// We only want to run this in the main repo
			if:        "${{github.repository == '\(core.#githubRepositoryPath)'}}"
			"runs-on": _#linuxMachine
			steps: [
				json.#step & {
					let branchPatterns = strings.Join(_#protectedBranchPatterns, " ")

					// rerunLatestWorkflow runs the latest trybot workflow in the
					// specified repo for branches that match the specified branch.
					let rerunLatestWorkflow = {
						#repo:   string
						#branch: string
						"""
						id=$(\(_base.#curlGitHubAPI) "https://api.github.com/repos/\(#repo)/actions/workflows/trybot.yml/runs?branch=\(#branch)&event=push&per_page=1" | jq '.workflow_runs[] | .id')
						\(_base.#curlGitHubAPI) -X POST https://api.github.com/repos/\(#repo)/actions/runs/$id/rerun

						"""
					}

					run: """
						set -eux

						echo ${{ secrets.CUECKOO_GITHUB_PAT }} | gh auth login --with-token
						gh extension install actions/gh-actions-cache
						for i in \(core.#githubRepositoryURL) \(core.#githubRepositoryURL)-trybot
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
						for j in $(\(_base.#curlGitHubAPI) -f https://api.github.com/repos/\(core.#githubRepositoryPath)/branches | jq -r '.[] | .name')
						do
							for i in \(branchPatterns)
							do
								if [[ "$j" != $i ]]; then
									continue
								fi

								echo "$j is a match with $i"
								\(rerunLatestWorkflow & {#repo: core.#githubRepositoryPath, #branch: "$j", _})
								\(rerunLatestWorkflow & {#repo: core.#githubRepositoryPath + "-trybot", #branch: "$j", _})
							done
						done
						"""
				},
			]
		}
	}
}
