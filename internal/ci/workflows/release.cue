package workflows

import (
	"strconv"
	encjson "encoding/json"
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

release: _core.#bashWorkflow & {

	name: "Release"
	on: push: tags: [_#releaseTagPattern]
	jobs: goreleaser: {
		"runs-on": _#linuxMachine
		steps: [
			_core.#checkoutCode & {
				with: "fetch-depth": 0
			},
			_core.#installGo & {
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
					\#(_gerritHub.#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-lang/cuelang.org/dispatches
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
					\#(_gerritHub.#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-unity/unity/dispatches
					"""#
			},
		]
	}
}
