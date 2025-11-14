// Copyright  CUE Authors
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

package filetypes

import (
	"cuelang.org/go/cue/build"
)

//go:generate go run -tags cuebootstrap ./generate.go

func toFile(mode Mode, sc *scope, filename string) (*build.File, error) {
	return toFileGenerated(mode, sc, filename)
}

// FromFile returns detailed file info for a given build file. It ignores b.Tags and
// b.BoolTags, instead assuming that any tag handling has already been processed
// by [ParseArgs] or similar.
// The b.Encoding field must be non-empty.
func FromFile(b *build.File, mode Mode) (*FileInfo, error) {
	return fromFileGenerated(b, mode)
}
