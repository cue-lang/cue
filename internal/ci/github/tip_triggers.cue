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

package github

// The tip_triggers workflow. This fires for each new commit that hits the
// default branch.
workflows: tip_triggers: _repo.bashWorkflow & {

	name: "Triggers on push to tip"
	on: push: branches: [_repo.defaultBranch]
	jobs: push: {
		"runs-on": _repo.linuxMachine
		if:        "${{github.repository == '\(_repo.githubRepositoryPath)'}}"
		steps: [
			_repo.workflowDispatch & {
				name:                  "Trigger tip.cuelang.org deploy"
				#githubRepositoryPath: _repo.cuelangRepositoryPath

				// #workflowID corresponds to the filename of the update_tip
				// workflow in the cue-lang/cuelang.org repo. If we could constrain
				// this across repos we would.
				#workflowID: "update_tip.yml"
			},
			_repo.repositoryDispatch & {
				name:                          "Trigger unity build"
				#githubRepositoryPath:         _repo.unityRepositoryPath
				#botGitHubUserTokenSecretsKey: "PORCUEPINE_GITHUB_PAT"
				#arg: {
					event_type: "Check against ${GITHUB_SHA}"
					client_payload: {
						type: "unity"
						payload: {
							versions: """
							"commit:${GITHUB_SHA}"
							"""
						}
					}
				}
			},
		]
	}
}
