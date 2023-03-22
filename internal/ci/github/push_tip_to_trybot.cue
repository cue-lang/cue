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
	"cuelang.org/go/internal/ci/core"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// push_tip_to_trybot "syncs" active branches to the trybot repo.
// Since the workflow is triggered by a push to any of the branches,
// the step only needs to sync the pushed branch.
workflows: push_tip_to_trybot: _base.#bashWorkflow & {

	name: "Push tip to trybot"
	on: {
		push: branches: _#protectedBranchPatterns
	}

	concurrency: "push_tip_to_trybot"

	jobs: push: {
		"runs-on": _#linuxMachine
		if:        "${{github.repository == '\(core.githubRepositoryPath)'}}"
		steps: [
			_gerrithub.#writeNetrcFile,
			json.#step & {
				name: "Push tip to trybot"
				run:  """
						mkdir tmpgit
						cd tmpgit
						git init
						git config user.name \(_gerrithub.#botGitHubUser)
						git config user.email \(_gerrithub.#botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(_gerrithub.#botGitHubUser):${{ secrets.\(_gerrithub.#botGitHubUserTokenSecretsKey) }} | base64)"
						git remote add origin \(_gerrithub.#gerritHubRepository)
						git remote add trybot \(_gerrithub.#trybotRepositoryURL)
						git fetch origin "${{ github.ref }}"
						git push trybot "FETCH_HEAD:${{ github.ref }}"
						"""
			},
		]
	}

}
