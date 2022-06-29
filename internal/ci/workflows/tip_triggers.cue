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
)

// The tip_triggers workflow. This fires for each new commit that hits the
// default branch.
tip_triggers: _#core.#bashWorkflow & {

	name: "Push to tip triggers"
	on: push: branches: [_#defaultBranch]
	jobs: push: {
		"runs-on": _#linuxMachine
		steps: [
			{
				name: "Rebuild tip.cuelang.org"
				run:  "\(_gerritHub.#curl) -X POST -d {} https://api.netlify.com/build_hooks/${{ secrets.CuelangOrgTipRebuildHook }}"
			},
			{
				_#arg: {
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
				name: "Trigger unity build"
				run:  #"""
					\#(_gerritHub.#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-unity/unity/dispatches
					"""#
			},
		]
	}
}
