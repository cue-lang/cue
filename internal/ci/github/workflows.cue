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

// Note: the name of the workflows (and hence the corresponding .yaml filenames)
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
	// Adding this constraint here, whilst clear for the reader,
	// blows out evaluation time. This will be fixed as part of
	// the performance work which is covered under various issues.
	// [string]: json.#Workflow

	_repo.trybotWorkflows

	release:      _
	tip_triggers: _
	trybot_dispatch: #dummyDispatch: _dummyDispatch
})

_dummyDispatch: _repo.#dispatch & {
	type: _repo.trybot.key
}
