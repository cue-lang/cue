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
// These projects have a bot account that has both GitHub and GerritHub
// identities, and developers use github.com/cue-sh/tools/cmd/cueckoo to
// trigger trybots for GitHub Actions-based CI runs.
package gerrithub

import (
	encjson "encoding/json"
	"strconv"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#repository:                         string
#branchRefPrefix:                    string
#botGitHubUser:                      string
#botGitHubUserTokenSecretsKey:       string
#botGitHubUserEmail:                 string
#botGerritHubUser:                   *#botGitHubUser | string
#botGerritHubUserPasswordSecretsKey: string
#botGerritHubUserEmail:              *#botGitHubUserEmail | string

_#linuxMachine: "ubuntu-20.04"

// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
// TODO: they probably belong elsewhere
#dispatchRuntrybot: "runtrybot"
#dispatchUnity:     "unity"

#curl: "curl -f -s"

#trybotWorkflow: json.#Workflow & {
	jobs: {
		delete_build_branch: {
			"runs-on": _#linuxMachine
			if:        "${{ \(_#isCLCITestBranch) && always() }}"
			needs:     "test"
			steps: [
				json.#step & {
					run: """
						\(_#tempBotGitDir)
						git push \(#repository)-trybot :${GITHUB_REF#\(#branchRefPrefix)}
						"""
				},
			]
		}
	}

}

#dispatchTrybotWorkflow: json.#Workflow & {
	name: "Dispatch runtrybot"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		"\(#dispatchRuntrybot)": {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(#dispatchRuntrybot)' }}"
			steps: [
				json.#step & {
					name: "Trigger trybot"
					run:  """
						\(_#tempBotGitDir)
						git fetch https://review.gerrithub.io/a/cue-lang/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cue-lang/cue ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
				},
			]
		}
	}
}

// _#isCLCITestBranch is an expression that evaluates to true
// if the job is running as a result of a CL triggered CI build
_#isCLCITestBranch: "startsWith(github.ref, '\(#branchRefPrefix)ci/')"

_#startCLBuild: json.#step & {
	name: "Update Gerrit CL message with starting message"
	run:  (_#gerrit._#setCodeReview & {
		#args: {
			message: "Started the build... see progress at ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
		}
	}).res
}

_#failCLBuild: json.#step & {
	if:   "${{ \(_#isCLCITestBranch) && failure() }}"
	name: "Post any failures for this matrix entry"
	run:  (_#gerrit._#setCodeReview & {
		#args: {
			message: "Build failed for ${{ runner.os }}-${{ matrix.go-version }}; see ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }} for more details"
			labels: {
				"TryBot-Result": -1
			}
		}
	}).res
}

_#passCLBuild: json.#step & {
	name: "Update Gerrit CL message with success message"
	run:  (_#gerrit._#setCodeReview & {
		#args: {
			message: "Build succeeded for ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
			labels: {
				"TryBot-Result": 1
			}
		}
	}).res
}

_#gerrit: {
	// _#setCodeReview assumes that it is invoked from a job where
	// _#isCLCITestBranch is true
	_#setCodeReview: {
		#args: {
			tag:     "trybot"
			message: string
			labels?: {
				"TryBot-Result": int
			}
		}
		res: #"""
			\#(#curl) -u '\#(#botGerritHubUser):${{ secrets.\#(#botGerritHubUserPasswordSecretsKey) }}' --basic -H "Content-Type: application/json" --request POST --data \#(strconv.Quote(encjson.Marshal(#args))) https://review.gerrithub.io/a/changes/$(basename $(dirname $GITHUB_REF))/revisions/$(basename $GITHUB_REF)/review
			"""#
	}
}

// _#tempBotGitDir is a series of bash commands that establish
// a temporary directory, set the working directory as that
// temporary directory, and prime a .git configuration that
// allows the bot user to interact with GitHub.
_#tempBotGitDir: """
		mkdir tmpgit
		cd tmpgit
		git init
		git config user.name \(#botGitHubUser)
		git config user.email \(#botGitHubUserEmail)
		git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(#botGitHubUser):${{ secrets.\(#botGitHubUserTokenSecretsKey) }} | base64)"
		"""
