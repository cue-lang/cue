// Copyright 2021 The CUE Authors
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
	"github.com/SchemaStore/schemastore/src/schemas/json"
	encjson "encoding/json"
	"strconv"
)

workflowsDir: *"./" | string @tag(workflowsDir)

_#masterBranch:      "master"
_#releaseTagPattern: "v*"

workflows: [...{file: string, schema: (json.#Workflow & {})}]
workflows: [
	{
		file:   "trybot.yml"
		schema: trybot
	},
	{
		file:   "trybot_dispatch.yml"
		schema: trybot_dispatch
	},
	{
		file:   "release.yml"
		schema: release
	},
	{
		file:   "tip_triggers.yml"
		schema: tip_triggers
	},
]

trybot: _#bashWorkflow & {

	name: "TryBot"
	on: {
		push: {
			branches: ["**"] // any branch (including '/' namespaced branches)
			"tags-ignore": [_#releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		start: {
			"runs-on": _#linuxMachine
			steps: [...(_ & {if: "${{ \(_#isCLCITestBranch) }}"})]
			steps: [
				_#writeNetrcFile,
				_#startCLBuild,
			]
		}
		test: {
			needs:     "start"
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_#writeNetrcFile,
				_#installGo,
				_#checkoutCode & {
					// "pull_request" builds will by default use a merge commit,
					// testing the PR's HEAD merged on top of the master branch.
					// For consistency with Gerrit, avoid that merge commit entirely.
					// This doesn't affect "push" builds, which never used merge commits.
					with: ref: "${{ github.event.pull_request.head.sha }}"
				},
				_#earlyChecks,
				_#cacheGoModules,
				_#step & {
					if:  "${{ \(_#isMaster) }}"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_#goGenerate,
				_#goTest,
				_#goCheck,
				_#goTestRace & {
					if: "${{ matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)' }}"
				},
				_#checkGitClean,
				_#pullThroughProxy,
				_#failCLBuild,
			]
		}
		mark_ci_success: {
			"runs-on": _#linuxMachine
			if:        "${{ \(_#isCLCITestBranch) }}"
			needs:     "test"
			steps: [
				_#writeNetrcFile,
				_#passCLBuild,
			]
		}
		delete_build_branch: {
			"runs-on": _#linuxMachine
			if:        "${{ \(_#isCLCITestBranch) && always() }}"
			needs:     "test"
			steps: [
				_#step & {
					run: """
						\(_#tempCueckooGitDir)
						git push https://github.com/cue-lang/cue :${GITHUB_REF#\(_#branchRefPrefix)}
						"""
				},
			]
		}
	}

	// _#isCLCITestBranch is an expression that evaluates to true
	// if the job is running as a result of a CL triggered CI build
	_#isCLCITestBranch: "startsWith(github.ref, '\(_#branchRefPrefix)ci/')"

	// _#isMaster is an expression that evaluates to true if the
	// job is running as a result of a master commit push
	_#isMaster: "github.ref == '\(_#branchRefPrefix+_#masterBranch)'"

	_#pullThroughProxy: _#step & {
		name: "Pull this commit through the proxy on \(_#masterBranch)"
		run: """
			v=$(git rev-parse HEAD)
			cd $(mktemp -d)
			go mod init mod.com
			GOPROXY=https://proxy.golang.org go get -d cuelang.org/go/cmd/cue@$v
			"""
		if: "${{ \(_#isMaster) }}"
	}

	_#startCLBuild: _#step & {
		name: "Update Gerrit CL message with starting message"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Started the build... see progress at ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
			}
		}).res
	}

	_#failCLBuild: _#step & {
		if:   "${{ \(_#isCLCITestBranch) && failure() }}"
		name: "Post any failures for this matrix entry"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Build failed for ${{ runner.os }}-${{ matrix.go-version }}; see ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }} for more details"
				labels: {
					"TryBot-Result": -1
				}
			}
		}).res
	}

	_#passCLBuild: _#step & {
		name: "Update Gerrit CL message with success message"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Build succeeded for ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
				labels: {
					"TryBot-Result": 1
				}
			}
		}).res
	}

	_#gerrit: {
		// _#setCodeReview assumes that it is invoked from a job where
		// _#isCLCITestBranch is true
		_#setCodeReview: {
			#args: {
				tag:     "trybot"
				message: string
				labels?: {
					"TryBot-Result": int
				}
			}
			res: #"""
			\#(_#curl) -n -H "Content-Type: application/json" --request POST --data \#(strconv.Quote(encjson.Marshal(#args))) https://review.gerrithub.io/a/changes/$(basename $(dirname $GITHUB_REF))/revisions/$(basename $GITHUB_REF)/review
			"""#
		}
	}
}

trybot_dispatch: _#bashWorkflow & {
	// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
	_#runtrybot: "runtrybot"
	_#unity:     "unity"

	_#dispatchJob: _#job & {
		_#type:    string
		"runs-on": _#linuxMachine
		if:        "${{ github.event.client_payload.type == '\(_#type)' }}"
	}

	name: "TryBot Dispatch"
	on: ["repository_dispatch"]
	jobs: {
		"\(_#runtrybot)": _#dispatchJob & {
			_#type: _#runtrybot
			steps: [
				_#writeNetrcFile,
				_#step & {
					name: "Trigger trybot"
					run:  """
						\(_#tempCueckooGitDir)
						git fetch https://review.gerrithub.io/a/cue-lang/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cue-lang/cue ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
				},
			]
		}
	}
}

release: _#bashWorkflow & {

	name: "Release"
	on: push: tags: [_#releaseTagPattern]
	jobs: goreleaser: {
		"runs-on": _#linuxMachine
		steps: [
			_#checkoutCode & {
				with: "fetch-depth": 0
			},
			_#installGo & {
				with: "go-version": _#pinnedReleaseGo
			},
			_#step & {
				name: "Setup qemu"
				uses: "docker/setup-qemu-action@v1"
			},
			_#step & {
				name: "Set up Docker Buildx"
				uses: "docker/setup-buildx-action@v1"
			},
			_#step & {
				name: "Docker Login"
				uses: "docker/login-action@v1"
				with: {
					registry: "docker.io"
					username: "cueckoo"
					password: "${{ secrets.CUECKOO_DOCKER_PAT }}"
				}
			},
			_#step & {
				name: "Run GoReleaser"
				env: GITHUB_TOKEN: "${{ secrets.CUECKOO_GITHUB_PAT }}"
				uses: "goreleaser/goreleaser-action@v2"
				with: {
					args:    "release --rm-dist"
					version: "v1.8.2"
				}
			},
			_#step & {
				_#arg: {
					event_type: "Re-test post release of ${GITHUB_REF##refs/tags/}"
				}
				name: "Re-test cuelang.org"
				run:  #"""
					\#(_#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-lang/cuelang.org/dispatches
					"""#
			},
			_#step & {
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
					\#(_#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-unity/unity/dispatches
					"""#
			},
		]
	}
}

tip_triggers: _#bashWorkflow & {

	name: "Push to tip triggers"
	on: push: branches: [_#masterBranch]
	jobs: push: {
		"runs-on": _#linuxMachine
		steps: [
			{
				name: "Rebuild tip.cuelang.org"
				run:  "\(_#curl) -X POST -d {} https://api.netlify.com/build_hooks/${{ secrets.CuelangOrgTipRebuildHook }}"
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
					\#(_#curl) -H "Content-Type: application/json" -u cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} --request POST --data-binary \#(strconv.Quote(encjson.Marshal(_#arg))) https://api.github.com/repos/cue-unity/unity/dispatches
					"""#
			},
		]
	}
}

_#bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

// TODO: drop when cuelang.org/issue/390 is fixed.
// Declare definitions for sub-schemas
_#job:  ((json.#Workflow & {}).jobs & {x: _}).x
_#step: ((_#job & {steps:                 _}).steps & [_])[0]

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
_#latestStableGo: "1.18.x"

// Use a specific latest version for release builds.
// Note that we don't want ".x" for the sake of reproducibility,
// so we instead pin a specific Go release.
_#pinnedReleaseGo: "1.18.1"

_#linuxMachine:   "ubuntu-20.04"
_#macosMachine:   "macos-11"
_#windowsMachine: "windows-2022"

_#testStrategy: {
	"fail-fast": false
	matrix: {
		"go-version": ["1.17.x", _#latestStableGo]
		os: [_#linuxMachine, _#macosMachine, _#windowsMachine]
	}
}

_#installGo: _#step & {
	name: "Install Go"
	uses: "actions/setup-go@v3"
	with: {
		"go-version": *"${{ matrix.go-version }}" | string
	}
}

_#checkoutCode: _#step & {
	name: "Checkout code"
	uses: "actions/checkout@v3"
}

_#earlyChecks: _#step & {
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

_#cacheGoModules: _#step & {
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

_#goGenerate: _#step & {
	name: "Generate"
	run:  "go generate ./..."
	// The Go version corresponds to the precise version specified in
	// the matrix. Skip windows for now until we work out why re-gen is flaky
	if: "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
}

_#goTest: _#step & {
	name: "Test"
	run:  "go test ./..."
}

_#goCheck: _#step & {
	// These checks can vary between platforms, as different code can be built
	// based on GOOS and GOARCH build tags.
	// However, CUE does not have any such build tags yet, and we don't use
	// dependencies that vary wildly between platforms.
	// For now, to save CI resources, just run the checks on one matrix job.
	// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
	if:   "matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)'"
	name: "Check"
	run:  "go vet ./..."
}

_#goTestRace: _#step & {
	name: "Test with -race"
	run:  "go test -race ./..."
}

_#checkGitClean: _#step & {
	name: "Check that git is clean post generate and tests"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

_#writeNetrcFile: _#step & {
	name: "Write netrc file for cueckoo Gerrithub"
	run: """
		cat <<EOD > ~/.netrc
		machine review.gerrithub.io
		login cueckoo
		password ${{ secrets.CUECKOO_GERRITHUB_PASSWORD }}
		EOD
		chmod 600 ~/.netrc
		"""
}

_#branchRefPrefix: "refs/heads/"

_#tempCueckooGitDir: """
	mkdir tmpgit
	cd tmpgit
	git init
	git config user.name cueckoo
	git config user.email cueckoo@gmail.com
	git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }} | base64)"
	"""

_#curl: "curl -f -s"
