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
// default branch or the default branch's test branch.
workflows: tip_triggers: _repo.bashWorkflow & {

	name: "Triggers on push to tip"
	on: push: branches: [_repo.defaultBranch, _repo.testDefaultBranch]
	jobs: push: {
		"runs-on": _repo.linuxMachine
		if:        "${{github.repository == '\(_repo.githubRepositoryPath)'}}"
		steps: [
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
			// This triggers the cuelang.org trybot, which finishes by testing that
			// the site builds successfully against the tip of cue-lang/cue.
			// The specific commit that triggered this workflow (inside tip_triggers)
			// isn't used by the cuelang.org build unless it also happens to be the
			// tip of cue-lang/cue, therefore there's no dispatch payload/etc that
			// communicates the commit ref.
			_repo.workflowDispatch & {
				name:                          "Trigger cuelang.org trybot"
				#githubRepositoryPath:         _repo.cuelangRepositoryPath
				#botGitHubUserTokenSecretsKey: "CUECKOO_GITHUB_PAT"
				#workflowID:                   "trybot.yaml"
			},
		]
	}
}
