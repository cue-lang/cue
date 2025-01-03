// Copyright 2024 The CUE Authors
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

// _registryReadOnlyAccessStep defines a step that configures
// a read-only Central Registry access credential. The actual
// command should be placed in the _run field.
_registryReadOnlyAccessStep: _repo.bashStep & {
	_run!: string
	env: {
		// Note: this token has read-only access to the registry
		// and is used only because we need some credentials
		// to pull dependencies from the Central Registry.
		// The token is owned by notcueckoo and described as "ci readonly".
		CUE_TOKEN: "${{ secrets.NOTCUECKOO_CUE_TOKEN }}"
	}
	// For now we `go run` cue to not rely on a previous `go install ./cmd/cue`
	// to have happened, which is very easy to forget or misconfigure.
	// We use the full import path so that this works from any module subdirectory.
	// TODO(mvdan): switch to `go tool cue` as soon as we are able to.
	#run: """
		go run cuelang.org/go/cmd/cue login --token=${CUE_TOKEN}
		\(_run)
		"""
}
