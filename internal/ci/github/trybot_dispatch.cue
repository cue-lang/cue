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

trybot_dispatch: _#bashWorkflow & {
	// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
	_#runtrybot: "runtrybot"
	_#unity:     "unity"

	_#dispatchJob: _#job & {
		_#type:    string
		"runs-on": _#linuxMachine
		if:        "${{ github.event.client_payload.type == '\(_#type)' }}"
	}

	name: "Repository Dispatch"
	on: ["repository_dispatch"]
	jobs: {
		"\(_#runtrybot)": _#dispatchJob & {
			_#type: _#runtrybot
			steps: [
				_#step & {
					name: "Trigger trybot"
					run:  """
						\(_#tempCueckooGitDir)
						git fetch https://review.gerrithub.io/a/cue-lang/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b trybot/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cue-lang/cue-trybot trybot/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
				},
			]
		}
	}
}
