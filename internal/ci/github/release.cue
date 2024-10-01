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

import (
	"list"

	"github.com/cue-tmp/jsonschema-pub/exp1/githubactions"
)

// _cueVersionRef is a workflow job-runtime expression that evaluates to the
// git tag (version) that is being released. Defining as a "constant" here for
// re-use below
_cueVersionRef: "${GITHUB_REF##refs/tags/}"

// The release workflow
workflows: release: _repo.bashWorkflow & {

	name: "Release"

	// We only want a single release happening at a time to avoid
	// race conditions on updating the latest docker images or
	// homebrew tags.
	concurrency: "release"

	on: push: {
		tags: [_repo.releaseTagPattern, "!" + _repo.zeroReleaseTagPattern]
		branches: list.Concat([[_repo.testDefaultBranch], _repo.protectedBranchPatterns])
	}
	jobs: goreleaser: {
		"runs-on": _repo.linuxMachine
		if:        "${{github.repository == '\(_repo.githubRepositoryPath)'}}"

		let installGo = _repo.installGo & {
			#setupGo: with: "go-version": _repo.pinnedReleaseGo
			_
		}
		steps: [
			for v in _repo.checkoutCode {v},
			for v in installGo {v},
			githubactions.#Step & {
				name: "Setup qemu"
				uses: "docker/setup-qemu-action@v3"
			},
			githubactions.#Step & {
				name: "Set up Docker Buildx"
				uses: "docker/setup-buildx-action@v3"
			},
			githubactions.#Step & {
				name: "Docker Login"
				uses: "docker/login-action@v3"
				with: {
					registry: "docker.io"
					username: "cueckoo"
					password: "${{ secrets.CUECKOO_DOCKER_PAT }}"
				}
			},
			githubactions.#Step & {
				name: "Install CUE"
				run:  "go install ./cmd/cue"
			},
			githubactions.#Step & {
				name: "Install GoReleaser"
				uses: "goreleaser/goreleaser-action@v5"
				with: {
					"install-only": true
					version:        _repo.goreleaserVersion
				}
			},
			_registryReadOnlyAccessStep & {
				// Note that the logic for what gets run at release time
				// is defined with the release command in CUE.
				name: "Run GoReleaser with CUE"
				env: GITHUB_TOKEN: "${{ secrets.CUECKOO_GITHUB_PAT }}"
				_run:                "cue cmd release"
				"working-directory": "./internal/ci/goreleaser"
			},
			_repo.repositoryDispatch & {
				name:                  "Re-test cuelang.org"
				if:                    _repo.isReleaseTag
				#githubRepositoryPath: _repo.cuelangRepositoryPath
				#arg: {
					event_type: "Re-test post release of \(_cueVersionRef)"
				}
			},
			_repo.repositoryDispatch & {
				name:                          "Trigger unity build"
				if:                            _repo.isReleaseTag
				#githubRepositoryPath:         _repo.unityRepositoryPath
				#botGitHubUserTokenSecretsKey: "PORCUEPINE_GITHUB_PAT"
				#arg: {
					event_type: "Check against CUE \(_cueVersionRef)"
					client_payload: {
						type: "unity"
						payload: {
							versions: """
							"\(_cueVersionRef)"
							"""
						}
					}
				}
			},
		]
	}
}
