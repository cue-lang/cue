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
	"cuelang.org/go/internal/ci/base"
	"cuelang.org/go/internal/ci/gerrithub"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// Note: the name of the workflows (and hence the corresponding .yml filenames)
// correspond to the environment variable names for gerritstatusupdater.
// Therefore, this filename must only be change in combination with also
// updating the environment in which gerritstatusupdater is running for this
// repository.
//
// This name is also used by the CI badge in the top-level README.
//
// This name is also used in the evict_caches lookups.
//
// i.e. don't change the names of workflows!
//
// In addition to separately declaring the workflows themselves, we define the
// shape of #workflows here as a cross-check that we don't accidentally change
// the name of workflows without reading this comment.
//
// We explicitly use close() here instead of a definition in order that we can
// cue export the github package as a test.
workflows: close({
	[string]: json.#Workflow

	(_base.#trybot.key): _
	trybot_dispatch:     _
	release:             _
	tip_triggers:        _
	push_tip_to_trybot:  _
	evict_caches:        _
})

// _gerrithub is an instance of ./gerrithub, parameterised by the properties of
// this project
_gerrithub: gerrithub & {
	#repositoryURL:                      core.githubRepositoryURL
	#trybotKey:                          _base.#trybot.key
	#trybotTrailer:                      _base.#trybot.trailer
	#botGitHubUser:                      "cueckoo"
	#botGitHubUserTokenSecretsKey:       "CUECKOO_GITHUB_PAT"
	#botGitHubUserEmail:                 "cueckoo@gmail.com"
	#botGerritHubUser:                   #botGitHubUser
	#botGerritHubUserPasswordSecretsKey: "CUECKOO_GERRITHUB_PASSWORD"
	#botGerritHubUserEmail:              #botGitHubUserEmail
}

// _base is an instance of ./base, parameterised by the properties of this
// project
//
// TODO: revisit the naming strategy here. _base and base are very similar.
// Perhaps rename the import to something more obviously not intended to be
// used, and then rename the field base?
_base: base & {
	#repositoryURL:                core.githubRepositoryURL
	#defaultBranch:                core.defaultBranch
	#botGitHubUser:                "cueckoo"
	#botGitHubUserTokenSecretsKey: "CUECKOO_GITHUB_PAT"
	#protectedBranchPatterns:      core.protectedBranchPatterns
	#releaseTagPattern:            core.releaseTagPattern
}
