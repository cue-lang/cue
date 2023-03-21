// Copyright 2022 The CUE Authors
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

// package gerritHub is a collection of features that are common to projects
// that choose to make GerritHub their source of truth, using GitHub Actions
// for CI.
//
// See the documentation for gerritstatusupdater for more information:
//
//   github.com/cue-lang/cuelang.org/internal/functions/gerritstatusupdater
//
package gerrithub

import (
	"path"
	"strings"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#repositoryURL:                      string
#gerritHubRepositoryURL:             string
#trybotKey:                          string
#trybotTrailer:                      string
#trybotRepositoryURL:                *(#repositoryURL + "-" + #trybotKey) | string
#botGitHubUser:                      string
#botGitHubUserTokenSecretsKey:       string
#botGitHubUserEmail:                 string
#botGerritHubUser:                   *#botGitHubUser | string
#botGerritHubUserPasswordSecretsKey: string
#botGerritHubUserEmail:              *#botGitHubUserEmail | string

// Pending cuelang.org/issue/1433, hack around defaulting #gerritHubRepository
// based on #repository
let _#repositoryURLNoScheme = strings.Split(#repositoryURL, "//")[1]
#gerritHubRepository: *("https://\(_#gerritHubHostname)/a/" + path.Base(path.Dir(_#repositoryURLNoScheme)) + "/" + path.Base(_#repositoryURLNoScheme)) | _

_#gerritHubHostname: "review.gerrithub.io"

_#linuxMachine: "ubuntu-20.04"

#trybotWorkflow: json.#Workflow & {
	name: "Dispatch \(#trybotKey)"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		(#trybotKey): {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(#trybotKey)' }}"
			steps: [
				#writeNetrcFile,
				json.#step & {
					name: "Trigger \(#trybotKey)"

					let targetBranchExpr = "${{ github.event.client_payload.targetBranch }}"
					let refsExpr = "${{ github.event.client_payload.refs }}"

					run: """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(#botGitHubUser)
						git config user.email \(#botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(#botGitHubUser):${{ secrets.\(#botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(#gerritHubRepository) "\(refsExpr)"
						git checkout -b \(targetBranchExpr) FETCH_HEAD

						# Fail if we already have a trybot trailer
						currTrailer="$(git log -1 --pretty='%(trailers:key=\(#trybotTrailer),valueonly)')"
						if [[ "$currTrailer" != "" ]]; then
							echo "Commit for refs \(refsExpr) already has \(#trybotTrailer)"
							exit 1
						fi

						trailer="$(cat <<EOD | tr '\\n' ' '
						${{ toJSON(github.event.client_payload) }}
						EOD
						)"

						git log -1 --format=%B | git interpret-trailers --trailer "\(#trybotTrailer): $trailer" | git commit --amend -F -

						for try in {1..20}; do
							echo $try
						  	git push -f \(#trybotRepositoryURL) \(targetBranchExpr):\(targetBranchExpr) && break
						  	sleep 1
						done
						"""
				},
			]
		}
	}
}

#writeNetrcFile: json.#step & {
	name: "Write netrc file for cueckoo Gerrithub"
	run:  """
			cat <<EOD > ~/.netrc
			machine \(_#gerritHubHostname)
			login \(#botGerritHubUser)
			password ${{ secrets.\(#botGerritHubUserPasswordSecretsKey) }}
			EOD
			chmod 600 ~/.netrc
			"""
}
