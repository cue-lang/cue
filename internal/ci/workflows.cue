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

package ci

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
	encjson "encoding/json"
)

workflowsDir: *"./" | string @tag(workflowsDir)

_#masterBranch:      "master"
_#releaseTagPattern: "v*"

workflows: [...{file: string, schema: (json.#Workflow & {})}]
workflows: [
	{
		file:   "test.yml"
		schema: test
	},
	{
		file:   "repository_dispatch.yml"
		schema: repository_dispatch
	},
	{
		file:   "release.yml"
		schema: release
	},
	{
		file:   "rebuild_tip_cuelang_org.yml"
		schema: rebuild_tip_cuelang_org
	},
	{
		file:   "mirror.yml"
		schema: mirror
	},
]

test: _#bashWorkflow & {

	name: "Test"
	on: {
		push: {
			branches: ["**"] // any branch (including '/' namespaced branches)
			"tags-ignore": [_#releaseTagPattern]
		}
	}

	jobs: {
		start: {
			"runs-on": _#linuxMachine
			steps: [...(_ & {if: "${{ \(_#isCLCITestBranch) }}"})]
			steps: [
				_#writeCookiesFile,
				_#startCLBuild,
			]
		}
		test: {
			needs:     "start"
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_#writeCookiesFile,
				_#installGo,
				_#checkoutCode,
				_#cacheGoModules,
				_#setGoBuildTags & {
					_#tags: "long"
					if:     "${{ \(_#isMaster) }}"
				},
				_#goGenerate,
				_#goTest,
				_#goTestRace & {
					if: "${{ \(_#isMaster) || \(_#isCLCITestBranch) && matrix.go-version == '\(_#latestStableGo)' && matrix.os == '\(_#linuxMachine)' }}"
				},
				_#goReleaseCheck,
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
				_#writeCookiesFile,
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
						git push https://github.com/cuelang/cue :${GITHUB_REF#\(_#branchRefPrefix)}
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
				labels: {
					"Code-Review": 0
				}
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
					"Code-Review": -1
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
					"Code-Review": 1
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
				labels: {
					"Code-Review": int
				}
			}
			res: #"""
			curl -f -s -H "Content-Type: application/json" --request POST --data '\#(encjson.Marshal(#args))' -b ~/.gitcookies https://cue-review.googlesource.com/a/changes/$(basename $(dirname $GITHUB_REF))/revisions/$(basename $GITHUB_REF)/review
			"""#
		}
	}
}

repository_dispatch: _#bashWorkflow & {
	// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
	_#runtrybot: "runtrybot"
	_#mirror:    "mirror"
	_#importpr:  "importpr"
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
						git fetch https://cue-review.googlesource.com/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cuelang/cue ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
				},
			]
		}
		"\(_#mirror)": _#dispatchJob & {
			_#type: _#mirror
			steps:  _#copybaraSteps & {_
				_#name: "Mirror Gerrit to GitHub"
				_#cmd:  "github"
			}
		}
		"\(_#importpr)": _#dispatchJob & {
			_#type: _#importpr
			steps:  _#copybaraSteps & {_
				_#name: "Import PR #${{ github.event.client_payload.commit }} from GitHub to Gerrit"
				_#cmd:  "github-pr ${{ github.event.client_payload.payload.pr }}"
			}
		}
	}
}

mirror: _#bashWorkflow & {
	name: "Scheduled repo mirror"
	on:
		schedule: [{
			cron: "*/30 * * * *" // every 30 mins
		}]

	jobs: {
		"mirror": {
			"runs-on": _#linuxMachine
			steps:     _#copybaraSteps & {_
				_#name: "Mirror Gerrit to GitHub"
				_#cmd:  "github"
			}
		}
	}
}

release: _#bashWorkflow & {

	name: "Release"
	on: push: tags: [_#releaseTagPattern]
	jobs: {
		goreleaser: {
			"runs-on": _#linuxMachine
			steps: [
				_#checkoutCode & {
					with: "fetch-depth": 0
				},
				_#installGo & {
					with: version: _#latestStableGo
				},
				_#step & {
					name: "Run GoReleaser"
					env: GITHUB_TOKEN: "${{ secrets.ACTIONS_GITHUB_TOKEN }}"
					uses: "goreleaser/goreleaser-action@v2"
					with: {
						args:    "release --rm-dist"
						version: "v0.155.1"
					}
				},
			]
		}
		docker: {
			name:      "docker"
			"runs-on": _#linuxMachine
			steps: [
				_#checkoutCode,
				_#step & {
					name: "Set version environment"
					run: """
						CUE_VERSION=$(echo ${GITHUB_REF##refs/tags/v})
						echo \"CUE_VERSION=$CUE_VERSION\"
						echo \"CUE_VERSION=$(echo $CUE_VERSION)\" >> $GITHUB_ENV
						"""
				},
				_#step & {
					name: "Push to Docker Hub"
					env: {
						DOCKER_BUILDKIT: 1
						GOLANG_VERSION:  1.14
						CUE_VERSION:     "${{ env.CUE_VERSION }}"
					}
					uses: "docker/build-push-action@v1"
					with: {
						tags:           "${{ env.CUE_VERSION }},latest"
						repository:     "cuelang/cue"
						username:       "${{ secrets.DOCKER_USERNAME }}"
						password:       "${{ secrets.DOCKER_PASSWORD }}"
						tag_with_ref:   false
						tag_with_sha:   false
						target:         "cue"
						always_pull:    true
						build_args:     "GOLANG_VERSION=${{ env.GOLANG_VERSION }},CUE_VERSION=v${{ env.CUE_VERSION }}"
						add_git_labels: true
					}
				},
			]
		}
	}
}

rebuild_tip_cuelang_org: _#bashWorkflow & {

	name: "Push to tip"
	on: push: branches: [_#masterBranch]
	jobs: push: {
		"runs-on": _#linuxMachine
		steps: [{
			name: "Rebuild tip.cuelang.org"
			run:  "curl -f -X POST -d {} https://api.netlify.com/build_hooks/${{ secrets.CuelangOrgTipRebuildHook }}"
		}]
	}
}

_#bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

// TODO: drop when cuelang.org/issue/390 is fixed.
// Declare definitions for sub-schemas
_#job:  ((json.#Workflow & {}).jobs & {x: _}).x
_#step: ((_#job & {steps:                 _}).steps & [_])[0]

// We need at least go1.14 for code generation
_#codeGenGo: "1.14.14"

// Use a specific latest version for release builds
_#latestStableGo: "1.15.8"

_#linuxMachine:   "ubuntu-18.04"
_#macosMachine:   "macos-10.15"
_#windowsMachine: "windows-2019"

_#testStrategy: {
	"fail-fast": false
	matrix: {
		// Use a stable version of 1.14.x for go generate
		"go-version": [_#codeGenGo, _#latestStableGo, "1.16"]
		os: [_#linuxMachine, _#macosMachine, _#windowsMachine]
	}
}

_#setGoBuildTags: _#step & {
	_#tags: string
	name:   "Set go build tags"
	run:    """
		go env -w GOFLAGS=-tags=\(_#tags)
		"""
}

_#installGo: _#step & {
	name: "Install Go"
	uses: "actions/setup-go@v2"
	with: {
		"go-version": *"${{ matrix.go-version }}" | string
		stable:       false
	}
}

_#checkoutCode: _#step & {
	name: "Checkout code"
	uses: "actions/checkout@v2"
}

_#cacheGoModules: _#step & {
	name: "Cache Go modules"
	uses: "actions/cache@v1"
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
	if: "matrix.go-version == '\(_#codeGenGo)' && matrix.os != '\(_#windowsMachine)'"
}

_#goTest: _#step & {
	name: "Test"
	run:  "go test ./..."
}

_#goTestRace: _#step & {
	name: "Test with -race"
	run:  "go test -race ./..."
}

_#goReleaseCheck: _#step & {
	name: "gorelease check"
	run:  "go run golang.org/x/exp/cmd/gorelease"
}

_#checkGitClean: _#step & {
	name: "Check that git is clean post generate and tests"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

_#writeCookiesFile: _#step & {
	name: "Write the gitcookies file"
	run:  "echo \"${{ secrets.gerritCookie }}\" > ~/.gitcookies"
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

// The cueckoo/copybara Docker image to use
_#cueckooCopybaraImage: "cueckoo/copybara:afc4ae03eed00b0c9d7415141cd1b5dfa583da7c"

// Define the base command for copybara
_#copybaraCmd: {
	_#cmd: string
	#"""
		cd _scripts
		docker run --rm -v $PWD/cache:/root/copybara/cache -v $PWD:/usr/src/app --entrypoint="" \#(_#cueckooCopybaraImage) bash -c " \
			set -eu; \
			echo \"${{ secrets.gerritCookie }}\" > ~/.gitcookies; \
			chmod 600 ~/.gitcookies; \
			git config --global user.name cueckoo; \
			git config --global user.email cueckoo@gmail.com; \
			git config --global http.cookiefile \$HOME/.gitcookies; \
		  	echo https://cueckoo:${{ secrets.CUECKOO_GITHUB_PAT }}@github.com > ~/.git-credentials; \
			chmod 600 ~/.git-credentials; \
			java -jar /opt/copybara/copybara_deploy.jar migrate copy.bara.sky \#(_#cmd); \
			"
		"""#
}

_#copybaraSteps: {
	_#name: string
	_#cmd:  string
	let cmdCmd = _#cmd
	[
		_#checkoutCode, // needed for copy.bara.sky file
		_#step & {
			name: _#name
			run:  _#copybaraCmd & {_, _#cmd: cmdCmd}
		},
	]
}
