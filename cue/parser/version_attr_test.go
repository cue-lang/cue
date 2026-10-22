// Copyright 2026 The CUE Authors
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

package parser_test

import (
	"regexp"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cueversion"
)

func TestVersionAttribute(t *testing.T) {
	// A prefix alias is valid before v0.18.0, where aliasv2 became stable.
	const body = "_x: X={a: 1, b: X.a}\n"
	current := cueversion.LanguageVersion()
	tests := []struct {
		name    string
		src     string
		version string // parser.Version option; empty for none
		want    string // the file's language version
		wantErr string
	}{{
		name: "BeforePackage",
		src:  "@version(\"v0.17.0\")\n\npackage p\n\n" + body,
		want: "v0.17.0",
	}, {
		name: "NoPackage",
		src:  "@version(\"v0.17.0\")\n\n" + body,
		want: "v0.17.0",
	}, {
		name: "Unquoted",
		src:  "@version(v0.17.0)\n\n" + body,
		want: "v0.17.0",
	}, {
		name:    "WinsOverOption",
		src:     "@version(\"v0.17.0\")\n\n" + body,
		version: "v0.16.0",
		want:    "v0.17.0",
	}, {
		name:    "CurrentRejected",
		src:     "@version(\"current\")\n\na: 1\n",
		wantErr: `invalid @version attribute: language version "current" is not canonical`,
	}, {
		name:    "NoAttribute",
		src:     body,
		version: "v0.17.0",
		want:    "v0.17.0",
	}, {
		// Field attributes are unaffected.
		name: "FieldAttribute",
		src:  "a: 1 @version(anything)\n",
		want: current,
	}, {
		// The file's experiments apply at its declared version.
		name:    "WithExperiment",
		src:     "@version(\"v0.17.0\")\n@experiment(aliasv2)\n\n" + body,
		wantErr: "old-style alias syntax",
	}, {
		name:    "AfterExperiment",
		src:     "@experiment(try)\n@version(\"v0.17.0\")\n\na: 1\n",
		wantErr: "@version must be the first file attribute, before the package clause, any @experiment attribute, and any declaration",
	}, {
		name:    "AfterPackage",
		src:     "package p\n\n@version(\"v0.17.0\")\n\nimport \"strings\"\n\na: strings.ToUpper(\"a\")\n",
		wantErr: "@version must be the first file attribute",
	}, {
		name:    "AfterDeclaration",
		src:     "a: 1\n\n@version(\"v0.17.0\")\n",
		wantErr: "@version must be the first file attribute",
	}, {
		name:    "Repeated",
		src:     "@version(\"v0.17.0\")\n@version(\"v0.17.0\")\n\na: 1\n",
		wantErr: "repeated @version attribute",
	}, {
		name:    "AfterOtherAttribute",
		src:     "@foo()\n@version(\"v0.17.0\")\n\npackage p\n\na: 1\n",
		wantErr: "@version must be the first file attribute",
	}, {
		name:    "TwoValues",
		src:     "@version(\"v0.17.0\", \"v9.0.0\")\n\na: 1\n",
		wantErr: "invalid @version attribute: want a single language version",
	}, {
		name: "CommentBefore",
		src:  "// A reproducer.\n@version(\"v0.17.0\")\n\n" + body,
		want: "v0.17.0",
	}, {
		name:    "NotCanonical",
		src:     "@version(\"v0.17\")\n\na: 1\n",
		wantErr: `invalid @version attribute: language version "v0.17" is not canonical`,
	}, {
		name:    "TooEarly",
		src:     "@version(\"v0.7.0\")\n\na: 1\n",
		wantErr: `invalid @version attribute: language version "v0.7.0" is too early`,
	}, {
		name:    "TooLate",
		src:     "@version(\"v9.0.0\")\n\na: 1\n",
		wantErr: `invalid @version attribute: language version "v9.0.0" may not be after current language version`,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var opts []parser.Option
			if test.version != "" {
				opts = append(opts, parser.Version(test.version))
			}
			f, err := parser.ParseFile("x.cue", test.src, opts...)
			if test.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, ".*"+regexp.QuoteMeta(test.wantErr)+".*"))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(f.Pos().LanguageVersion(), test.want))
		})
	}
}
