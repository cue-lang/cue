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

package workflows

import (
	"strconv"
	encjson "encoding/json"

	"cuelang.org/go/internal/ci/workflows/core"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The release workflow
release: core.#bashWorkflow & {

	name: "Release"
	on: push: tags: [_#releaseTagPattern]
	jobs: goreleaser: {
		"runs-on": _#linuxMachine
		steps: [
			core.#checkoutCode & {
				with: "fetch-depth": 0
			},
			core.#installGo & {
				with: "go-version": _#pinnedReleaseGo
			},
			json.#step & {
				name: "Setup qemu"
				uses: "docker/setup-qemu-action@v1"
			},
			json.#step & {
				name: "Set up Docker Buildx"
				uses: "docker/setup-buildx-action@v1"
			},
			json.#step & {
				name: "Docker Login"
				uses: "docker/login-action@v1"
				with: {
					registry: "docker.io"
					username: "cueckoo"
					password: "${{ secrets.CUECKOO_DOCKER_PAT }}"
				}
			},
			json.#step & {
				name: "Run GoReleaser"
				env: GITHUB_TOKEN: "${{ secrets.CUECKOO_GITHUB_PAT }}"
				uses: "goreleaser/goreleaser-action@v2"
				with: {
					args:    "release --rm-dist"
					version: "v1.8.2"
				}
			},
			json.#step & {
				_#arg: {
					event_type: "Re-test post release of ${GITHUB_REF##refs/tags/}"
				}
				name: "Re-test cuelang.org"
				run:  #"""
					\#(_gerrithub.#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-lang/cuelang.org/dispatches
					"""#
			},
			json.#step & {
				_#arg: {
					event_type: "Check against CUE ${GITHUB_REF##refs/tags/}"
					client_payload: {
						type: "unity"
						payload: {
							versions: """
							"${GITHUB_REF##refs/tags/}"
							"""
						}
					}
				}
				name: "Trigger unity build"
				run:  #"""
					\#(_gerrithub.#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-unity/unity/dispatches
					"""#
			},
		]
	}
}
