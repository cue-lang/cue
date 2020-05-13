package ci

import (
	"github.com/SchemaStore/schemastore/schemas/json"
)

workflowsDir: *"./" | string @tag(workflowsDir)

workflows: [...{file: string, schema: json.Workflow}]
workflows: [
	{
		file:   "test.yml"
		schema: test
	},
	{
		file:   "test_dispatch.yml"
		schema: test_dispatch
	},
	{
		file:   "release.yml"
		schema: release
	},
	{
		file:   "rebuild_tip_cuelang_org.yml"
		schema: rebuild_tip_cuelang_org
	},
]

test: json.Workflow & {
	name: "Test"
	on: {
		push: {
			branches: ["*"]
			"tags-ignore": ["v*"]
		}
	}
	defaults: run: shell: "bash"
	jobs: test: {
		strategy: {
			"fail-fast": false
			matrix: {
				// Use a stable version of 1.14.x for the go generate step below
				"go-version": ["1.13.x", "1.14.3"]
				os: ["ubuntu-latest", "macos-latest", "windows-latest"]
			}
		}
		"runs-on": "${{ matrix.os }}"
		steps: [{
			name: "Install Go"
			uses: "actions/setup-go@v2"
			with: "go-version": "${{ matrix.go-version }}"
		}, {
			name: "Checkout code"
			uses: "actions/checkout@v2"
		}, {
			name: "Cache Go modules"
			uses: "actions/cache@v1"
			with: {
				path: "~/go/pkg/mod"
				key:  "${{ runner.os }}-${{ matrix.go-version }}-go-${{ hashFiles('**/go.sum') }}"
				"restore-keys": """
					${{ runner.os }}-${{ matrix.go-version }}-go-
					"""
			}
		}, {
			name: "Generate"
			run: """
				go generate ./...
				go generate ./.github/workflows
				"""
			// The Go version corresponds to the precise 1.14.x version specified in
			// the matrix. Skip windows for now until we work out why re-gen is flaky
			if: "matrix.go-version == '1.14.3' && matrix.os != 'windows-latest'"
		}, {
			name: "Test"
			run:  "go test ./..."
		}, {
			name: "Test with -race"
			run:  "go test -race ./..."
		}, {
			name: "gorelease check"
			run:  "go run golang.org/x/exp/cmd/gorelease"
		}, {
			name: "Check that git is clean post generate and tests"
			run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
		}, {
			name: "Pull this commit through the proxy on master"
			run: """
				v=$(git rev-parse HEAD)
				cd $(mktemp -d)
				go mod init mod.com
				GOPROXY=https://proxy.golang.org go get -d cuelang.org/go@$v
				"""
			if: "github.ref == 'refs/heads/master'"
		}]
	}
}

test_dispatch: json.Workflow & {

	name: "Test"
	env: GERRIT_COOKIE: "${{ secrets.gerritCookie }}"
	on: ["repository_dispatch"]
	defaults: run: shell: "bash"
	jobs: {
		start: {
			"runs-on": "ubuntu-latest"
			steps: [{
				name: "Write the gitcookies file"
				run:  "echo \"$GERRIT_COOKIE\" > ~/.gitcookies"
			}, {
				name: "Update Gerrit CL message with starting message"
				run: """
					curl -f -s -H \"Content-Type: application/json\" --request POST --data '{\"message\":\"Started the build... see progress at ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}\"}' -b ~/.gitcookies https://cue-review.googlesource.com/a/changes/${{ github.event.client_payload.changeID }}/revisions/${{ github.event.client_payload.commit }}/review
					"""
			}]
		}
		test: {
			"runs-on": "${{ matrix.os }}"
			steps: [{
				name: "Write the gitcookies file"
				run:  "echo \"$GERRIT_COOKIE\" > ~/.gitcookies"
			}, {
				name: "Install Go"
				uses: "actions/setup-go@v2"
				with: "go-version": "${{ matrix.go-version }}"
			}, {
				name: "Checkout code"
				uses: "actions/checkout@v2"
			}, {
				name: "Checkout ref"
				run: """
				  git fetch https://cue-review.googlesource.com/cue ${{ github.event.client_payload.ref }}
				  git checkout FETCH_HEAD
				  """
			}, {
				name: "Cache Go modules"
				uses: "actions/cache@v1"
				with: {
					path: "~/go/pkg/mod"
					key:  "${{ runner.os }}-${{ matrix.go-version }}-go-${{ hashFiles('**/go.sum') }}"
					"restore-keys": """
						${{ runner.os }}-${{ matrix.go-version }}-go-
						"""
				}
			}, {
				name: "Generate"
				run: """
						go generate ./...
						go generate ./.github/workflows
						"""
				// The Go version corresponds to the precise 1.14.x version specified in
				// the matrix. Skip windows for now until we work out why re-gen is flaky
				if:   "matrix.go-version == '1.14.3' && matrix.os != 'windows-latest'"
			}, {
				name: "Test"
				run:  "go test ./..."
			}, {
				name: "Test with -race"
				run:  "go test -race ./..."
			}, {
				name: "gorelease check"
				run:  "go run golang.org/x/exp/cmd/gorelease"
			}, {
				name: "Check that git is clean post generate and tests"
				run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
			}, {
				name: "Post any failures for this matrix entry"
				run: """
					curl -f -s -H \"Content-Type: application/json\" --request POST --data '{\"labels\": { \"Code-Review\": -1 }, \"message\":\"Build failed for ${{ runner.os }}-${{ matrix.go-version }}; see ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }} for more details\"}' -b ~/.gitcookies https://cue-review.googlesource.com/a/changes/${{ github.event.client_payload.changeID }}/revisions/${{ github.event.client_payload.commit }}/review
					"""
				if: "${{ failure() }}"
			}]
			needs: "start"
			strategy: {
				"fail-fast": false
				matrix: {
					// Use a stable version of 1.14.x for the go generate step below
					"go-version": ["1.13.x", "1.14.3"]
					os: ["ubuntu-latest", "macos-latest", "windows-latest"]
				}
			}
		}
		end: {
			"runs-on": "ubuntu-latest"
			steps: [{
				name: "Write the gitcookies file"
				run:  "echo \"$GERRIT_COOKIE\" > ~/.gitcookies"
			}, {
				name: "Update Gerrit CL message with success message"
				run: """
					curl -f -s -H \"Content-Type: application/json\" --request POST --data '{\"labels\": { \"Code-Review\": 1 }, \"message\":\"Build succeeded for ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}\"}' -b ~/.gitcookies https://cue-review.googlesource.com/a/changes/${{ github.event.client_payload.changeID }}/revisions/${{ github.event.client_payload.commit }}/review
					"""
			}]
			needs: "test"
		}
	}
}
release: {
	name: "Release"
	on: push: tags: ["v*"]
	jobs: {
		goreleaser: {
			"runs-on": "ubuntu-latest"
			steps: [{
				name: "Checkout code"
				uses: "actions/checkout@v2"
			}, {
				name: "Unshallow" // required for the changelog to work correctly.
				run:  "git fetch --prune --unshallow"
			}, {
				name: "Run GoReleaser"
				env: GITHUB_TOKEN: "${{ secrets.ACTIONS_GITHUB_TOKEN }}"
				uses: "docker://goreleaser/goreleaser:latest"
				with: args: "release --rm-dist"
			}]
		}
		docker: {
			name:      "docker"
			"runs-on": "ubuntu-latest"
			steps: [{
				name: "Check out the repo"
				uses: "actions/checkout@v2"
			}, {
				name: "Set version environment"
				run: """
					CUE_VERSION=$(echo ${GITHUB_REF##refs/tags/v})
					echo \"CUE_VERSION=$CUE_VERSION\"
					echo \"::set-env name=CUE_VERSION::$(echo $CUE_VERSION)\"
					"""
			}, {
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
			}]
		}
	}
}

rebuild_tip_cuelang_org: json.Workflow & {
	name: "Push to tip"
	on: push: branches: ["master"]
	jobs: push: {
		"runs-on": "ubuntu-latest"
		steps: [{
			name: "Rebuild tip.cuelang.org"
			run:  "curl -f -X POST -d {} https://api.netlify.com/build_hooks/${{ secrets.CuelangOrgTipRebuildHook }}"
		}]
	}
}
