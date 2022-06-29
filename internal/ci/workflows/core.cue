package workflows

import (
	"strconv"
	encjson "encoding/json"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

_core: {
	#triggerUnity: json.#step & {
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
	}
	#bashWorkflow: json.#Workflow & {
		jobs: [string]: defaults: run: shell: "bash"
	}

	#installGo: json.#step & {
		name: "Install Go"
		uses: "actions/setup-go@v3"
		with: {
			"go-version": *"${{ matrix.go-version }}" | string
		}
	}

	#checkoutCode: json.#step & {
		name: "Checkout code"
		uses: "actions/checkout@v3"
	}

	#earlyChecks: json.#step & {
		name: "Early git and code sanity checks"
		run: """
			# Ensure the recent commit messages have Signed-off-by headers.
			# TODO: Remove once this is enforced for admins too;
			# see https://bugs.chromium.org/p/gerrit/issues/detail?id=15229
			# TODO: Our --max-count here is just 1, because we've made mistakes very
			# recently. Increase it to 5 or 10 soon, to also cover CL chains.
			for commit in $(git rev-list --max-count=1 HEAD); do
				if ! git rev-list --format=%B --max-count=1 $commit | grep -q '^Signed-off-by:'; then
					echo -e "\nRecent commit is lacking Signed-off-by:\n"
					git show --quiet $commit
					exit 1
				fi
			done
			"""
		// These checks don't vary based on the Go version or OS,
		// so we only need to run them on one of the matrix jobs.
		if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
	}

	#cacheGoModules: json.#step & {
		name: "Cache Go modules"
		uses: "actions/cache@v3"
		with: {
			path: "~/go/pkg/mod"
			key:  "${{ runner.os }}-${{ matrix.go-version }}-go-${{ hashFiles('**/go.sum') }}"
			"restore-keys": """
				${{ runner.os }}-${{ matrix.go-version }}-go-
				"""
		}
	}

	#checkGitClean: json.#step & {
		name: "Check that git is clean post generate and tests"
		run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
	}
}
