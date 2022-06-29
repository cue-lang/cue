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
//
// For such projects, the setup is as follows:
//
// * Project github.com/my/project defines at least two workflows: a test
//   workflow that should be run for each CL/PR, and a repository dispatch
//   workflow that is fired by running cmd/cueckoo runtrybot (see below).
// * github.com/my/project is imported into GerritHub as
//   review.gerrithub.io/q/project:my/project. github.com/my/project is
//   now the mirror target of the GerritHub project.
// * github.com/my/project-trybot is established as the repository within
//   which CI runs for CLs against review.gerrithub.io/q/project:my/project.
//   This repository has no secrets and is basically an empty shell that
//   defines a placeholder for the test workflow.
// * Developers with write access to github.com/my/project use cmd/cueckoo to
//   run trybots for CLs against review.gerrithub.io/q/project:my/project. This
//   pushes a build branch named ci/$changeID/$revisionID to
//   github.com/my/project-trybot. This triggers the test workflow
// * Workflow events triggered by the running of the test workflow fire webook
//   events. The github.com/cue-lang/cuelang.org/internal/functions/gerritstatusupdater
//   serverless function is configured in github.com/my/project-trybot as a
//   consumer of those events.
// * According to the configuration of gerritstatusupdater, those webhook
//   events are converted into status updates on the CL that corresponds to the
//   originating build branch (the Gerrit API works using the $changeID and
//   $revisionID).
//
// This package provides helper configuration for projects like
// github.com/my/project.
//
// The only constraint that must be satisfied between github.com/my/project and
// github.com/my/project-trybot is that the latter must define empty/shell
// GitHub Actions workflows for the workflow that will run as part of the
// trybots.  Indeed it is currently a limitation that only one workflow can be
// triggered (this constraint comes about because gerritstatusupdater cannot
// multiplex events from multiple disconnected workflows). Such an empty shell
// would look something like this:
//
//    # .github/workfows/test.yml
//    name: Test
//    "on":
//      push:
//        branches:
//          - 'ci/**'
//    jobs:
//      start:
//        runs-on: ubuntu-20.04
//        defaults:
//          run:
//            shell: bash
//        steps:
//          - run: 'echo hello world'
//
// github.com/my/project would then have a workflow configuration such that
// for a trybot run, the .github/workfows/test.yml file in the pushed build
// branch would be run.
package gerrithub

import (
	"path"
	"strings"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

#repositoryURL:                      string
#gerritHubRepositoryURL:             string
#trybotRepositoryURL:                *(#repositoryURL + "-trybot") | string
#botGitHubUser:                      string
#botGitHubUserTokenSecretsKey:       string
#botGitHubUserEmail:                 string
#botGerritHubUser:                   *#botGitHubUser | string
#botGerritHubUserPasswordSecretsKey: string
#botGerritHubUserEmail:              *#botGitHubUserEmail | string

// Pending cuelang.org/issue/1433, hack around defaulting #gerritHubRepository
// based on #repository
let _#repositoryURLNoScheme = strings.Split(#repositoryURL, "//")[1]
#gerritHubRepository: *("https://review.gerrithub.io/a/" + path.Base(path.Dir(_#repositoryURLNoScheme)) + "/" + path.Base(_#repositoryURLNoScheme)) | _

_#linuxMachine: "ubuntu-20.04"

// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
// TODO: they probably belong elsewhere
#dispatchTrybot: "trybot"
#dispatchUnity:  "unity"

#dispatchWorkflow: json.#Workflow & {
	#type:                  #dispatchTrybot | #dispatchUnity
	_#branchNameExpression: "\(#type)/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}"
	name:                   "Dispatch \(#type)"
	on: ["repository_dispatch"]
	jobs: [string]: defaults: run: shell: "bash"
	jobs: {
		"\(#type)": {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(#type)' }}"
			steps: [
				json.#step & {
					name: "Trigger trybot"
					run:  """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(#botGitHubUser)
						git config user.email \(#botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(#botGitHubUser):${{ secrets.\(#botGitHubUserTokenSecretsKey) }} | base64)"
						git fetch \(#gerritHubRepository) ${{ github.event.client_payload.payload.ref }}
						git checkout -b \(_#branchNameExpression) FETCH_HEAD
						git push \(#trybotRepositoryURL) \(_#branchNameExpression)
						"""
				},
			]
		}
	}
}
