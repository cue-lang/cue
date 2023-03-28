// Copyright 2022 The CUE Authors
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

// This file exists to provide a single point of importing
// the repo package. The pattern of using base and repo
// is replicated across a number of CUE repos, and as such
// the import path of repo varies between them. This makes
// spotting differences and applying changes between the
// github/*.cue files noisy. Instead, import the repo package
// in a single file, and that keeps the different in import
// path down to a single file.

import repo "cuelang.org/go/internal/ci/repo"

_repo: repo
