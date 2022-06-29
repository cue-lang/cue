package workflows

import (
	"strconv"
	encjson "encoding/json"
)

tip_triggers: _core.#bashWorkflow & {

	name: "Push to tip triggers"
	on: push: branches: [_#defaultBranch]
	jobs: push: {
		"runs-on": _#linuxMachine
		steps: [
			{
				name: "Rebuild tip.cuelang.org"
				run:  "\(_gerritHub.#curl) -X POST -d {} https://api.netlify.com/build_hooks/${{ secrets.CuelangOrgTipRebuildHook }}"
			},
			_core.#triggerUnity,
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
