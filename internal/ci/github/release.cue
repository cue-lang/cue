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

	"cuelang.org/go/internal/ci/core"
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// _#cueVersionRef is a workflow job-runtime expression that evaluates to the
// git tag (version) that is being released. Defining as a "constant" here for
// re-use below
_#cueVersionRef: "${GITHUB_REF##refs/tags/}"

// The release workflow
workflows: release: core.#bashWorkflow & {

	name: "Release"

	// We only want a single release happening at a time to avoid
	// race conditions on updating the latest docker images or
	// homebrew tags.
	concurrency: "release"

	on: push: {
		tags: [core.releaseTagPattern, "!" + core.zeroReleaseTagPattern]
		branches: list.Concat([[core.testDefaultBranch], core.protectedBranchPatterns])
	}
	jobs: goreleaser: {
		"runs-on": core.linuxMachine
		if:        "${{github.repository == '\(core.githubRepositoryPath)'}}"
		steps: [
			for v in core.#checkoutCode {v},
			core.#installGo & {
				with: "go-version": core.pinnedReleaseGo
			},
			json.#step & {
				name: "Setup qemu"
				uses: "docker/setup-qemu-action@v2"
			},
			json.#step & {
				name: "Set up Docker Buildx"
				uses: "docker/setup-buildx-action@v2"
			},
			json.#step & {
				name: "Docker Login"
				uses: "docker/login-action@v2"
				with: {
					registry: "docker.io"
					username: "cueckoo"
					password: "${{ secrets.CUECKOO_DOCKER_PAT }}"
				}
			},
			json.#step & {
				name: "Install CUE"
				run:  "go install ./cmd/cue"
			},
			json.#step & {
				name: "Install GoReleaser"
				uses: "goreleaser/goreleaser-action@v3"
				with: {
					"install-only": true
					version:        core.goreleaserVersion
				}
			},
			json.#step & {
				// Note that the logic for what gets run at release time
				// is defined with the release command in CUE.
				name: "Run GoReleaser with CUE"
				env: GITHUB_TOKEN: "${{ secrets.CUECKOO_GITHUB_PAT }}"
				run:                 "cue cmd release"
				"working-directory": "./internal/ci/goreleaser"
			},
			core.#repositoryDispatch & {
				name:           "Re-test cuelang.org"
				if:             core.#isReleaseTag
				#repositoryURL: "https://github.com/cue-lang/cuelang.org"
				#arg: {
					event_type: "Re-test post release of \(_#cueVersionRef)"
				}
			},
			core.#repositoryDispatch & {
				name:           "Trigger unity build"
				if:             core.#isReleaseTag
				#repositoryURL: core.unityRepositoryURL
				#arg: {
					event_type: "Check against CUE \(_#cueVersionRef)"
					client_payload: {
						type: "unity"
						payload: {
							versions: """
							"\(_#cueVersionRef)"
							"""
						}
					}
				}
			},
		]
	}
}
