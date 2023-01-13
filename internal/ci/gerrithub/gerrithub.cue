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
#trybotRepositoryURL:                *(#repositoryURL + "-" + #dispatchTrybot) | string
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

// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
// TODO: they probably belong elsewhere
#dispatchTrybot: "trybot"
#dispatchUnity:  "unity"

#dispatchWorkflow: json.#Workflow & {
	#type:                  #dispatchTrybot | #dispatchUnity
	_#branchNameExpression: "\(#type)/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                   "Dispatch \(#type)"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		"\(#type)": {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(#type)' }}"
			steps: [
				#writeNetrcFile,
				// Hack to get the ref (e.g. refs/changes/38/547738/7) in a format we can use in a
				// branch name, e.g. _547738_7
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
						git config user.name \(#botGitHubUser)
						git config user.email \(#botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(#botGitHubUser):${{ secrets.\(#botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(#gerritHubRepository) "${{ github.event.client_payload.payload.ref }}"
						git checkout -b \(_#branchNameExpression) FETCH_HEAD
						git remote add origin \(#trybotRepositoryURL)
						git fetch origin "${{ github.event.client_payload.payload.branch }}"
						git push origin \(_#branchNameExpression)
						echo ${{ secrets.CUECKOO_GITHUB_PAT }} | gh auth login --with-token
						gh pr --repo=\(#trybotRepositoryURL) create --base="${{ github.event.client_payload.payload.branch }}" --fill
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
