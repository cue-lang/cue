package workflows

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

test: _gerritHub.#trybotWorkflow & {
	name: "Test"

	on: {
		push: {
			branches: ["**"] // any branch (including '/' namespaced branches)
			"tags-ignore": [_#releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			strategy: _#testStrategy
			#steps: [
				_core.#installGo,
				_core.#checkoutCode & {
					// "pull_request" builds will by default use a merge commit,
					// testing the PR's HEAD merged on top of the master branch.
					// For consistency with Gerrit, avoid that merge commit entirely.
					// This doesn't affect "push" builds, which never used merge commits.
					with: ref: "${{ github.event.pull_request.head.sha }}"
				},
				_core.#earlyChecks,
				_core.#cacheGoModules,
				json.#step & {
					if:  "${{ \(_#isDefaultBranch) }}"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_#goGenerate,
				_#goTest,
				_#goCheck,
				_#goTestRace & {
					if: "${{ matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)' }}"
				},
				_core.#checkGitClean,
				_#pullThroughProxy,
			]
		}
	}

	// _#isDefaultBranch is an expression that evaluates to true if the
	// job is running as a result of a master commit push
	_#isDefaultBranch: "github.ref == '\(_#branchRefPrefix+_#defaultBranch)'"

	_#pullThroughProxy: json.#step & {
		name: "Pull this commit through the proxy on \(_#defaultBranch)"
		run: """
			v=$(git rev-parse HEAD)
			cd $(mktemp -d)
			go mod init mod.com
			GOPROXY=https://proxy.golang.org go get -d cuelang.org/go/cmd/cue@$v
			"""
		if: "${{ \(_#isDefaultBranch) }}"
	}

}
