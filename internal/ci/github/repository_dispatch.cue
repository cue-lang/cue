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
