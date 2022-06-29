package workflows

import (
	"cuelang.org/go/internal/ci/workflows/core"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The repository_dispatch workflow.
repository_dispatch: core.#bashWorkflow & {
	name: "Dispatch runtrybot"
	on: ["repository_dispatch"]
	jobs: {
		"\(_gerrithub.#dispatchRuntrybot)": {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(_gerrithub.#dispatchRuntrybot)' }}"
			steps: [
				json.#step & {
					name: "Trigger trybot"
					run:  """
						\(_gerrithub.#tempBotGitDir)
						git fetch https://review.gerrithub.io/a/cue-lang/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cue-lang/cue ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
				},
			]
		}
	}
}
