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

import (
	"github.com/cue-tmp/jsonschema-pub/exp1/githubactions"
)

// _registryReadOnlyAccessStep defines a step that configures
// a read-only Central Registry access credential. The actual
// command should be placed in the _run field.
_registryReadOnlyAccessStep: githubactions.#Step & {
	_run!: string
	env: {
		// Note: this token has read-only access to the registry
		// and is used only because we need some credentials
		// to pull dependencies from the Central Registry.
		CUE_LOGINS: "${{ secrets.NOTCUECKOO_CUE_LOGINS }}"
	}
	run: """
		export CUE_CONFIG_DIR=$(mktemp -d)
		echo "$CUE_LOGINS" > $CUE_CONFIG_DIR/logins.json
		\(_run)
		"""
}
