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

// package github declares the workflows for this project.
package github

import (
	"cuelang.org/go/internal/ci/core"
	"cuelang.org/go/internal/ci/gerrithub"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

workflowsDir: *"./" | string @tag(workflowsDir)

_#defaultBranch:     "master"
_#releaseTagPattern: "v*"
_#branchRefPrefix:   "refs/heads/"

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
		file:   "tip_triggers.yml"
		schema: tip_triggers
	},
]

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

// _gerrithub is an instance of ./gerrithub, parameterised by the properties of
// this project
_gerrithub: gerrithub & {
	// TODO: #repository #gerritHubRepository should come from codereview.cfg
	#repository:                         "https://github.com/cue-lang/cue"
	#branchRefPrefix:                    _#branchRefPrefix
	#botGitHubUser:                      "cueckoo"
	#botGitHubUserTokenSecretsKey:       "CUECKOO_GITHUB_PAT"
	#botGitHubUserEmail:                 "cueckoo@gmail.com"
	#botGerritHubUser:                   #botGitHubUser
	#botGerritHubUserPasswordSecretsKey: "CUECKOO_GERRITHUB_PASSWORD"
	#botGerritHubUserEmail:              #botGitHubUserEmail
}

// _core is an instance of ./core, parameterised by the properties of this
// project
_core: core & {
	#repository:                   "https://github.com/cue-lang/cue"
	#branchRefPrefix:              _#branchRefPrefix
	#defaultBranch:                _#defaultBranch
	#botGitHubUser:                "cueckoo"
	#botGitHubUserTokenSecretsKey: "CUECKOO_GITHUB_PAT"
}
