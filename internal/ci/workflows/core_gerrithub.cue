package workflows

import (
	"list"
	"github.com/SchemaStore/schemastore/src/schemas/json"
	encjson "encoding/json"
	"strconv"
)

_#gerritHub: {
	#repository:                         string
	#botGitHubUser:                      string
	#botGitHubUserTokenSecretsKey:       string
	#botGitHubUserEmail:                 string
	#botGerritHubUser:                   *#botGitHubUser | string
	#botGerritHubUserPasswordSecretsKey: string
	#botGerritHubUserEmail:              *#botGitHubUserEmail | string

	// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
	// TODO: they probably belong elsewhere
	_#dispatchRuntrybot: "runtrybot"
	_#dispatchUnity:     "unity"

	#dispatch: json.#Workflow & {
		#type: string
		jobs: [string]: defaults: run: shell: "bash"
		name: "Dispatch runtrybot"
		on: ["repository_dispatch"]
		jobs: {
			"\(_#dispatchRuntrybot)": {
				_#type:    _#dispatchRuntrybot
				"runs-on": _#linuxMachine
				if:        "${{ github.event.client_payload.type == '\(_#type)' }}"
				steps: [
					json.#step & {
						name: "Trigger trybot"
						run:  """
						\(#tempBotGitDir)
						git fetch https://review.gerrithub.io/a/cue-lang/cue ${{ github.event.client_payload.payload.ref }}
						git checkout -b ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }} FETCH_HEAD
						git push https://github.com/cue-lang/cue ci/${{ github.event.client_payload.payload.changeID }}/${{ github.event.client_payload.payload.commit }}
						"""
					},
				]
			}
		}
	}

	#curl: "curl -f -s"

	#trybotWorkflow: json.#Workflow & {
		jobs: {
			start: {
				"runs-on": _#linuxMachine
				steps: [...(_ & {if: "${{ \(_#isCLCITestBranch) }}"})]
				steps: [
					_#startCLBuild,
				]
			}
			test: {
				#steps: [...json.#step]
				needs: "start"
				strategy: {
					matrix: {}
				}
				steps:     list.Concat([#steps, [_#failCLBuild]])
				"runs-on": "${{ matrix.os }}"
			}
			mark_ci_success: {
				"runs-on": _#linuxMachine
				if:        "${{ \(_#isCLCITestBranch) }}"
				needs:     "test"
				steps: [
					_#passCLBuild,
				]
			}
			delete_build_branch: {
				"runs-on": _#linuxMachine
				if:        "${{ \(_#isCLCITestBranch) && always() }}"
				needs:     "test"
				steps: [
					json.#step & {
						run: """
						\(#tempBotGitDir)
						git push https://github.com/cue-lang/cue :${GITHUB_REF#\(_#branchRefPrefix)}
						"""
					},
				]
			}
		}

	}

	// _#isCLCITestBranch is an expression that evaluates to true
	// if the job is running as a result of a CL triggered CI build
	_#isCLCITestBranch: "startsWith(github.ref, '\(_#branchRefPrefix)ci/')"

	_#startCLBuild: json.#step & {
		name: "Update Gerrit CL message with starting message"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Started the build... see progress at ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
			}
		}).res
	}

	_#failCLBuild: json.#step & {
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

	_#passCLBuild: json.#step & {
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
			\#(#curl) -u \#(#botGerritHubUser):{{ secrets.\#(#botGerritHubUserPasswordSecretsKey) }} --basic -H "Content-Type: application/json" --request POST --data \#(strconv.Quote(encjson.Marshal(#args))) https://review.gerrithub.io/a/changes/$(basename $(dirname $GITHUB_REF))/revisions/$(basename $GITHUB_REF)/review
			"""#
		}
	}

	#tempBotGitDir: """
		mkdir tmpgit
		cd tmpgit
		git init
		git config user.name \(#botGitHubUser)
		git config user.email \(#botGitHubUserEmail)
		git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(#botGitHubUser):${{ secrets.\(#botGitHubUserTokenSecretsKey) }} | base64)"
		"""
}
