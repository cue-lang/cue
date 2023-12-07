// Copyright 2023 CUE Authors
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

package module

import (
	"cuelang.org/go/internal/mod/semver"
)

// Versions implements mvs.Versions[Version].
type Versions struct{}

// New implements mvs.Versions[Version].Version.
func (Versions) Version(v Version) string {
	return v.Version()
}

// New implements mvs.Versions[Version].Path.
func (Versions) Path(v Version) string {
	return v.Path()
}

// New implements mvs.Versions[Version].New.
func (Versions) New(p, v string) (Version, error) {
	return NewVersion(p, v)
}

// Max implements mvs.Reqs.Max.
//
// It is consistent with semver.Compare except that as a special case,
// the version "" is considered higher than all other versions. The main
// module (also known as the target) has no version and must be chosen
// over other versions of the same module in the module dependency
// graph.
//
// See [mvs.Reqs] for more detail.
func (Versions) Max(v1, v2 string) string {
	if v1 == "none" || v2 == "" {
		return v2
	}
	if v2 == "none" || v1 == "" {
		return v1
	}
	if semver.Compare(v1, v2) > 0 {
		return v1
	}
	return v2
}
