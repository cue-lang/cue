// Copyright 2018 The CUE Authors
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

package load

import (
	"strings"
	"testing"
)

var matchPatternTests = `
	pattern ...
	match foo
	
	pattern net
	match net
	not net/http
	
	pattern net/http
	match net/http
	not net
	
	pattern net...
	match net net/http netchan
	not not/http not/net/http
	
	# Special cases. Quoting docs:

	# First, /... at the end of the pattern can match an empty string,
	# so that net/... matches both net and packages in its subdirectories, like net/http.
	pattern net/...
	match net net/http
	not not/http not/net/http netchan

	# Second, any slash-separted pattern element containing a wildcard never
	# participates in a match of the "vendor" element in the path of a vendored
	# package, so that ./... does not match packages in subdirectories of
	# ./vendor or ./mycode/vendor, but ./vendor/... and ./mycode/vendor/... do.
	# Note, however, that a directory named vendor that itself contains code
	# is not a vendored package: cmd/vendor would be a command named vendor,
	# and the pattern cmd/... matches it.
	pattern ./...
	match ./vendor ./mycode/vendor
	not ./vendor/foo ./mycode/vendor/foo
	
	pattern ./vendor/...
	match ./vendor/foo ./vendor/foo/vendor
	not ./vendor/foo/vendor/bar
	
	pattern mycode/vendor/...
	match mycode/vendor mycode/vendor/foo mycode/vendor/foo/vendor
	not mycode/vendor/foo/vendor/bar
	
	pattern x/vendor/y
	match x/vendor/y
	not x/vendor
	
	pattern x/vendor/y/...
	match x/vendor/y x/vendor/y/z x/vendor/y/vendor x/vendor/y/z/vendor
	not x/vendor/y/vendor/z
	
	pattern .../vendor/...
	match x/vendor/y x/vendor/y/z x/vendor/y/vendor x/vendor/y/z/vendor
`

func testPatterns(t *testing.T, name, tests string, fn func(string, string) bool) {
	var patterns []string
	for _, line := range strings.Split(tests, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		default:
			t.Fatalf("unknown directive %q", f[0])
		case "pattern":
			patterns = f[1:]
		case "match", "not":
			want := f[0] == "match"
			for _, pattern := range patterns {
				for _, in := range f[1:] {
					if fn(pattern, in) != want {
						t.Errorf("%s(%q, %q) = %v, want %v", name, pattern, in, !want, want)
					}
				}
			}
		}
	}
}
