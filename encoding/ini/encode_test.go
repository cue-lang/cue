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

package ini_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/ini"
)

func TestEncoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config ini.Config
		input  string
		// want is the INI output, without its final line feed.
		want    string
		wantErr string
	}{{
		name:  "EmptyStruct",
		input: ``,
		want:  ``,
	}, {
		name:  "StringRoot",
		input: `"x"`,
		wantErr: `
			cannot write string at the root: an INI document is a struct:
			    in.cue:1:1
			`,
	}, {
		name:  "ListRoot",
		input: `["x"]`,
		wantErr: `
			cannot write list at the root: an INI document is a struct:
			    in.cue:1:1
			`,
	}, {
		name:  "Incomplete",
		input: `a: string`,
		wantErr: `
			a: incomplete value string:
			    in.cue:1:4
			`,
	}, {
		name: "GlobalsBeforeSections",
		input: `
			a: "x"
			s: k: "v"
			b: "y"
			`,
		want: `
			a = x
			b = y

			[s]
			k = v
			`,
	}, {
		name: "SectionsInSourceOrder",
		input: `
			s2: {b: "2", a: "1"}
			s1: c: "3"
			`,
		want: `
			[s2]
			b = 2
			a = 1

			[s1]
			c = 3
			`,
	}, {
		name: "Scalars",
		input: `
			t:   true
			f:   false
			i:   42
			n:   -7
			x:   1.5
			big: 123456789012345678901234567890
			`,
		want: `
			t = true
			f = false
			i = 42
			n = -7
			x = 1.5
			big = 123456789012345678901234567890
			`,
	}, {
		name:  "EmptyString",
		input: `a: ""`,
		want:  `a =`,
	}, {
		name: "CommentCharactersInValue",
		input: `
			a: "x ; y # z"
			b: ";x"
			`,
		want: `
			a = x ; y # z
			b = ;x
			`,
	}, {
		name:   "FirstDelimiter",
		config: ini.Config{Delimiters: ":="},
		input:  `a: "x=y"`,
		want:   `a : x=y`,
	}, {
		name: "EmptySection",
		input: `
			a: "x"
			s: {}
			`,
		want: `
			a = x

			[s]
			`,
	}, {
		name: "OnlyRegularFields",
		input: `
			#d: k: "v"
			_h: "x"
			o?: "x"
			a:  "y"
			`,
		want: `a = y`,
	}, {
		name:  "SurroundingSpaceLiteral",
		input: `a: " x "`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "SurroundingSpaceStripped",
		config: ini.Config{Quotes: ini.QuotesStripped},
		input:  `a: "  x "`,
		want:   `a = "  x "`,
	}, {
		name:   "QuotedStripped",
		config: ini.Config{Quotes: ini.QuotesStripped},
		input:  `a: "\"x\""`,
		want:   `a = ""x""`,
	}, {
		name:   "QuoteAndCommentStripped",
		config: ini.Config{Quotes: ini.QuotesStripped, Comments: ini.CommentsInline},
		input: `
			a: "a\" ;b"
			b: "x ;y"
			`,
		want: `
			a = 'a" ;b'
			b = "x ;y"
			`,
	}, {
		name:   "CommentLiteral",
		config: ini.Config{Comments: ini.CommentsInline},
		input:  `a: "x ;y"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "Escaped",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			path:  "C:\\dir"
			tab:   "a\tb\t"
			quote: "say \"hi\""
			space: " x"
			`,
		want: `
			path = C:\\dir
			tab = a\tb\t
			quote = say \"hi\"
			space = " x"
			`,
	}, {
		name:   "TypedStrings",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			s: "true"
			n: "42"
			b: true
			i: 42
			f: -1.5e3
			`,
		want: `
			s = "true"
			n = "42"
			b = true
			i = 42
			f = -1.5E+3
			`,
	}, {
		name:   "TypedStringLiteral",
		config: ini.Config{Values: ini.ValuesTyped},
		input:  `s: "true"`,
		wantErr: `
			cannot write string at s: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "ExtendedBooleans",
		config: ini.Config{Values: ini.ValuesTyped, Booleans: ini.BooleansExtended, Quotes: ini.QuotesStripped},
		input: `
			s: "yes"
			b: true
			`,
		want: `
			s = "yes"
			b = true
			`,
	}, {
		name:   "ExtendedBooleansNumber",
		config: ini.Config{Values: ini.ValuesTyped, Booleans: ini.BooleansExtended},
		input:  `i: 1`,
		wantErr: `
			cannot write number at i: it would decode as a boolean; set Config.Booleans to BooleansTrueFalse to allow it:
			    in.cue:1:1
			`,
	}, {
		name:  "KeyWithDelimiter",
		input: `"a=b": "x"`,
		wantErr: `
			cannot write key at "a=b": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "KeyLikeHeader",
		input: `"[a": "x"`,
		wantErr: `
			cannot write key at "[a": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "KeyLikeComment",
		input: `"#a": "x"`,
		wantErr: `
			cannot write key at "#a": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "KeyWithSpace",
		input: `" a": "x"`,
		wantErr: `
			cannot write key at " a": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "KeyWithLineBreak",
		input: `"a\nb": "x"`,
		wantErr: `
			cannot write key at "a\nb": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:   "KeyWithInlineComment",
		config: ini.Config{Comments: ini.CommentsInline},
		input:  `"a ;b": "x"`,
		wantErr: `
			cannot write key at "a ;b": no spelling decodes back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "SectionWithSpace",
		input: `"s ": k: "v"`,
		wantErr: `
			cannot write section at "s ": its header would not decode back to it:
			    in.cue:1:1
			`,
	}, {
		name:  "SectionWithEquals",
		input: `"a=b": k: "v"`,
		want: `
			[a=b]
			k = v
			`,
	}, {
		name:  "SectionWithBracket",
		input: `"a]b": k: "v"`,
		wantErr: `
			cannot write section at "a]b": its header would not decode back to it:
			    in.cue:1:1
			`,
	}, {
		name:   "SectionWithBracketTrailingText",
		config: ini.Config{TrailingHeaderText: true},
		input:  `"a]b": k: "v"`,
		want: `
			[a]b]
			k = v
			`,
	}, {
		name:  "NestedSection",
		input: `a: b: c: "x"`,
		wantErr: `
			cannot write section at a.b: nested sections need a nesting option; set Config.QuotedSubsections or Config.DottedSections to true to allow it:
			    in.cue:1:4
			`,
	}, {
		name:  "List",
		input: `a: ["x", "y"]`,
		wantErr: `
			cannot write list at a: lists are repeated keys; set Config.DuplicateKeys to DuplicatesList to allow it:
			    in.cue:1:1
			`,
	}, {
		name:  "Null",
		input: `a: null`,
		wantErr: `
			cannot write null at a: null is a bare key; set Config.BareKeys to BareKeysNull to allow it:
			    in.cue:1:1
			`,
	}, {
		name:  "Bytes",
		input: `a: 'x'`,
		wantErr: `
			cannot write bytes at a: INI has no bytes values:
			    in.cue:1:1
			`,
	}, {
		name:   "QuotedSubsection",
		config: ini.Config{QuotedSubsections: true},
		input:  `remote: origin: url: "x"`,
		want: `
			[remote "origin"]
			url = x
			`,
	}, {
		name:   "DottedSection",
		config: ini.Config{DottedSections: true},
		input:  `remote: origin: url: "x"`,
		want: `
			[remote.origin]
			url = x
			`,
	}, {
		name:   "QuotedPreferred",
		config: ini.Config{QuotedSubsections: true, DottedSections: true},
		input:  `remote: origin: url: "x"`,
		want: `
			[remote "origin"]
			url = x
			`,
	}, {
		name:   "DottedDepthThree",
		config: ini.Config{QuotedSubsections: true, DottedSections: true},
		input:  `a: b: c: k: "v"`,
		want: `
			[a.b.c]
			k = v
			`,
	}, {
		name:   "QuotedDepthThree",
		config: ini.Config{QuotedSubsections: true},
		input:  `a: b: c: k: "v"`,
		wantErr: `
			cannot write section at a.b.c: sections nested more than two deep need dotted headers; set Config.DottedSections to true to allow it:
			    in.cue:1:7
			`,
	}, {
		name:   "ImpliedHeaders",
		config: ini.Config{QuotedSubsections: true},
		input:  `remote: {origin: url: "x", upstream: url: "y"}`,
		want: `
			[remote "origin"]
			url = x

			[remote "upstream"]
			url = y
			`,
	}, {
		name:   "PropertiesBeforeSubsections",
		config: ini.Config{QuotedSubsections: true},
		input:  `s: {sub: k: "v", a: "1"}`,
		want: `
			[s]
			a = 1

			[s "sub"]
			k = v
			`,
	}, {
		name:   "EmptySubsection",
		config: ini.Config{QuotedSubsections: true},
		input:  `s: sub: {}`,
		want:   `[s "sub"]`,
	}, {
		name:   "SubsectionEscapes",
		config: ini.Config{QuotedSubsections: true},
		input:  `s: {"fix/\"q\"\\x": k: "v", "a]b.c": k: "w"}`,
		want: `
			[s "fix/\"q\"\\x"]
			k = v

			[s "a]b.c"]
			k = w
			`,
	}, {
		name:   "QuoteInSectionName",
		config: ini.Config{QuotedSubsections: true},
		input:  `"a\"b": k: "v"`,
		wantErr: `
			cannot write section at "a\"b": its header would not decode back to it:
			    in.cue:1:1
			`,
	}, {
		name:   "DotInDottedName",
		config: ini.Config{DottedSections: true},
		input:  `a: "b.c": k: "v"`,
		wantErr: `
			cannot write section at a."b.c": its header would not decode back to it:
			    in.cue:1:4
			`,
	}, {
		name:   "FoldedDottedParent",
		config: ini.Config{QuotedSubsections: true, DottedSections: true, Case: ini.CaseLower},
		input:  `a: B: {x: "1", c: y: "2"}`,
		wantErr: `
			cannot write section at a.B.c: its header would not decode back to it:
			    in.cue:1:16
			`,
	}, {
		name:   "ListRepeated",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input:  `a: ["x", "y"], s: b: ["1", 2, true]`,
		want: `
			a = x
			a = y

			[s]
			b = 1
			b = 2
			b = true
			`,
	}, {
		name:   "ListOneElement",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input:  `a: ["x"]`,
		want:   `a = x`,
	}, {
		name:   "EmptyList",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input:  `a: []`,
		wantErr: `
			cannot write empty list at a: no lines would decode back to it:
			    in.cue:1:1
			`,
	}, {
		name:   "ListOfStructs",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input:  `a: ["x", {b: "y"}]`,
		wantErr: `
			cannot write list at a: element 1 is a struct:
			    in.cue:1:1
			`,
	}, {
		name:   "ListOfLists",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input:  `a: [["x"]]`,
		wantErr: `
			cannot write list at a: element 0 is a list:
			    in.cue:1:1
			`,
	}, {
		name:   "ListFirst",
		config: ini.Config{DuplicateKeys: ini.DuplicatesFirst},
		input:  `a: ["x", "y"]`,
		wantErr: `
			cannot write list at a: lists are repeated keys; set Config.DuplicateKeys to DuplicatesList to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "ListLast",
		config: ini.Config{DuplicateKeys: ini.DuplicatesLast},
		input:  `a: ["x", "y"]`,
		wantErr: `
			cannot write list at a: lists are repeated keys; set Config.DuplicateKeys to DuplicatesList to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "NullBareKey",
		config: ini.Config{BareKeys: ini.BareKeysNull},
		input:  `a: null, b: "x"`,
		want: `
			a
			b = x
			`,
	}, {
		name:   "NullInList",
		config: ini.Config{BareKeys: ini.BareKeysNull, DuplicateKeys: ini.DuplicatesList},
		input:  `a: [null, "x"]`,
		want: `
			a
			a = x
			`,
	}, {
		name:   "NullBareKeysTrue",
		config: ini.Config{BareKeys: ini.BareKeysTrue},
		input:  `a: null`,
		wantErr: `
			cannot write null at a: null is a bare key; set Config.BareKeys to BareKeysNull to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "BooleansNeverBare",
		config: ini.Config{BareKeys: ini.BareKeysTrue},
		input:  `a: true, b: false`,
		want: `
			a = true
			b = false
			`,
	}, {
		name:   "NamesAsAuthored",
		config: ini.Config{Case: ini.CaseLower, QuotedSubsections: true},
		input:  `Core: {autoCRLF: "true", Sub: {Key: "v"}}`,
		want: `
			[Core]
			autoCRLF = true

			[Core "Sub"]
			Key = v
			`,
	}, {
		name:   "CollidingKeysLowerKeys",
		config: ini.Config{Case: ini.CaseLowerKeys},
		input:  `s: {Key: "1", key: "2"}`,
		wantErr: `
			cannot write name at s.key: s.Key has the same name under Config.Case; set Config.Case to CasePreserve to allow it:
			    in.cue:1:15
			`,
	}, {
		name:   "SectionsDistinctLowerKeys",
		config: ini.Config{Case: ini.CaseLowerKeys},
		input:  `A: {x: "1"}, a: {y: "2"}`,
		want: `
			[A]
			x = 1

			[a]
			y = 2
			`,
	}, {
		name:   "CollidingSectionsLower",
		config: ini.Config{Case: ini.CaseLower},
		input:  `A: {x: "1"}, a: {y: "2"}`,
		wantErr: `
			cannot write name at a: A has the same name under Config.Case; set Config.Case to CasePreserve to allow it:
			    in.cue:1:14
			`,
	}, {
		name:   "CollidingInsensitive",
		config: ini.Config{Case: ini.CaseInsensitive},
		input:  `s: {Key: "1", KEY: "2"}`,
		wantErr: `
			cannot write name at s.KEY: s.Key has the same name under Config.Case; set Config.Case to CasePreserve to allow it:
			    in.cue:1:15
			`,
	}, {
		name:   "CollidingPropertyAndSection",
		config: ini.Config{Case: ini.CaseLower},
		input:  `a: "1", A: b: "2"`,
		wantErr: `
			cannot write name at A: a has the same name under Config.Case; set Config.Case to CasePreserve to allow it:
			    in.cue:1:9
			`,
	}, {
		name:   "CollidingImpliedSections",
		config: ini.GitConfig(),
		input:  `a: {B: c: k: "x", b: c: k: "y"}`,
		wantErr: `
			cannot write name at a.b: a.B has the same name under Config.Case; set Config.Case to CasePreserve to allow it:
			    in.cue:1:19
			`,
	}, {
		name:   "QuotedSubsectionsNeverFold",
		config: ini.Config{Case: ini.CaseLower, QuotedSubsections: true},
		input:  `s: {A: x: "1", a: y: "2"}`,
		want: `
			[s "A"]
			x = 1

			[s "a"]
			y = 2
			`,
	}, {
		name:   "IndentedLines",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input:  `a: "one\ntwo", b: "\ntwo", c: "one\n\nthree"`,
		want: `
			a = one
				two
			b =
				two
			c = one

				three
			`,
	}, {
		name:   "IndentedTrailingLineBreak",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input:  `a: "one\n"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "IndentedLeadingSpace",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input:  `a: "one\n two"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "IndentedCommentLine",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input:  `a: "one\n#two"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "IndentedFallsBackToQuotes",
		config: ini.Config{Continuations: ini.ContinuationsIndented, Quotes: ini.QuotesEscaped},
		input:  `a: "one\n"`,
		want:   `a = one\n`,
	}, {
		name:   "BackslashEscaped",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Quotes: ini.QuotesEscaped},
		input:  `a: "one\ntwo"`,
		want:   `a = one\ntwo`,
	}, {
		name:   "BackslashSpaceLineBreak",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input:  `a: "one\ntwo"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "BackslashSpaceTrailingBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input:  `a: "C:\\"`,
		wantErr: `
			cannot write string at a: no spelling decodes back to it; set Config.Quotes to QuotesEscaped to allow it:
			    in.cue:1:1
			`,
	}, {
		name:   "GitSpellings",
		config: ini.GitConfig(),
		input:  `s: {nl: "a\nb", sp: " x", semi: "x;y", quote: "say \"hi\"", path: "C:\\dir\\"}`,
		want: `
			[s]
			nl = a\nb
			sp = " x"
			semi = "x;y"
			quote = say \"hi\"
			path = C:\\dir\\
			`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			v := cuecontext.New().CompileString(test.input, cue.Filename("in.cue"))
			qt.Assert(t, qt.IsNil(v.Err()))
			var out bytes.Buffer
			enc := ini.NewEncoder(&out, test.config)
			err := enc.Encode(v)

			if test.wantErr != "" {
				gotErr := strings.TrimSuffix(errors.Details(err, nil), "\n")
				qt.Assert(t, qt.Equals(gotErr, unindent(test.wantErr)))
				qt.Assert(t, qt.Equals(out.Len(), 0), qt.Commentf("an error writes nothing"))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			want := unindent(test.want)
			if want != "" {
				want += "\n"
			}
			qt.Assert(t, qt.Equals(out.String(), want))
			roundTrip(t, test.config, v, out.Bytes())

			// An INI file holds one document.
			err = enc.Encode(v)
			qt.Assert(t, qt.ErrorMatches(err, `cannot write a second value: an INI file holds one document`))
			qt.Assert(t, qt.Equals(out.String(), want), qt.Commentf("a second value writes nothing"))
		})
	}

	// Every tricky string, as a value, a key, and a section name, either
	// reads back as written or is an error naming its path, under the
	// generic flavor, each named flavor, and each single-option variant of
	// the generic flavor. A value is only spelled other than as its text
	// when that text would not read back.
	for _, cfg := range matrixConfigs {
		t.Run("Matrix/"+cfg.name, func(t *testing.T) {
			t.Parallel()
			ctx := cuecontext.New()
			for _, s := range trickyStrings {
				v := ctx.Encode(map[string]any{"k": s})
				out, err := encode(cfg.config, v)
				if err != nil {
					qt.Assert(t, qt.ErrorMatches(err, `cannot write string at k: .*`), qt.Commentf("value %q", s))
					qt.Assert(t, qt.Not(qt.Equals(cfg.config.Quotes, ini.QuotesEscaped)), qt.Commentf("value %q: escaped quotes spell every string", s))
				} else {
					roundTrip(t, cfg.config, v, out)
					delim := "="
					if cfg.config.Delimiters != "" {
						delim = cfg.config.Delimiters[:1]
					}
					if bare := "k " + delim + " " + s + "\n"; string(out) != bare && s != "" && !strings.Contains(s, "\n") {
						expr, err := ini.NewDecoder("bare.ini", strings.NewReader(bare), cfg.config).Decode()
						if err == nil {
							got := comparable(t, cfg.config, ctx.BuildExpr(expr))
							qt.Assert(t, qt.Not(qt.DeepEquals(got, comparable(t, cfg.config, v))), qt.Commentf("value %q is written as %q, but reads back bare", s, out))
						}
					}
				}

				v = ctx.Encode(map[string]any{s: "x"})
				if out, err := encode(cfg.config, v); err != nil {
					qt.Assert(t, qt.ErrorMatches(err, `cannot write key at .*`), qt.Commentf("key %q", s))
				} else {
					roundTrip(t, cfg.config, v, out)
				}

				v = ctx.Encode(map[string]any{s: map[string]any{"k": "x"}})
				if out, err := encode(cfg.config, v); err != nil {
					qt.Assert(t, qt.ErrorMatches(err, `cannot write section at .*`), qt.Commentf("section %q", s))
				} else {
					roundTrip(t, cfg.config, v, out)
				}
			}
		})
	}
}

// encode encodes v under cfg.
func encode(cfg ini.Config, v cue.Value) ([]byte, error) {
	var out bytes.Buffer
	err := ini.NewEncoder(&out, cfg).Encode(v)
	return out.Bytes(), err
}

// trickyStrings are the values and names the encoder matrix writes.
var trickyStrings = []string{
	"", "x", "a b", " x", "x ", "\tx", "ünï",
	";x", "#x", "x;y", "x#y", "x ;y", "x #y",
	`"x"`, `'x'`, `"x`, `x"y`, `say "hi" ;x`,
	`a\b`, `x\`, `x\\`, `a\nb`,
	"a\nb", "\nb", "a\n\nb", "a\n", "a\n b", "a\n#b",
	"true", "yes", "1", "42", "-1.5e3",
	"[x", "a=b", "a:b", "a.b", "a]b", "a[b]",
}

// matrixConfigs are the generic flavor, each named flavor, and each
// single-option variant of the generic flavor.
var matrixConfigs = []struct {
	name   string
	config ini.Config
}{
	{"Generic", ini.Config{}},
	{"Git", ini.GitConfig()},
	{"Python", ini.PythonConfig()},
	{"Systemd", ini.SystemdConfig()},
	{"Windows", ini.WindowsConfig()},
	{"DelimitersColon", ini.Config{Delimiters: ":="}},
	{"CommentsInline", ini.Config{Comments: ini.CommentsInline}},
	{"CommentsAnywhere", ini.Config{Comments: ini.CommentsAnywhere}},
	{"QuotesStripped", ini.Config{Quotes: ini.QuotesStripped}},
	{"QuotesEscaped", ini.Config{Quotes: ini.QuotesEscaped}},
	{"CaseLowerKeys", ini.Config{Case: ini.CaseLowerKeys}},
	{"CaseLower", ini.Config{Case: ini.CaseLower}},
	{"CaseInsensitive", ini.Config{Case: ini.CaseInsensitive}},
	{"DottedSections", ini.Config{DottedSections: true}},
	{"QuotedSubsections", ini.Config{QuotedSubsections: true}},
	{"TrailingHeaderText", ini.Config{TrailingHeaderText: true}},
	{"DuplicatesList", ini.Config{DuplicateKeys: ini.DuplicatesList}},
	{"DuplicatesFirst", ini.Config{DuplicateKeys: ini.DuplicatesFirst}},
	{"DuplicatesLast", ini.Config{DuplicateKeys: ini.DuplicatesLast}},
	{"DuplicateSectionsError", ini.Config{DuplicateSections: ini.DuplicateSectionsError}},
	{"DuplicateSectionsFirst", ini.Config{DuplicateSections: ini.DuplicateSectionsFirst}},
	{"ContinuationsBackslash", ini.Config{Continuations: ini.ContinuationsBackslash}},
	{"ContinuationsBackslashSpace", ini.Config{Continuations: ini.ContinuationsBackslashSpace}},
	{"ContinuationsIndented", ini.Config{Continuations: ini.ContinuationsIndented}},
	{"BareKeysNull", ini.Config{BareKeys: ini.BareKeysNull}},
	{"BareKeysTrue", ini.Config{BareKeys: ini.BareKeysTrue}},
	{"ValuesTyped", ini.Config{Values: ini.ValuesTyped}},
	{"BooleansExtended", ini.Config{Values: ini.ValuesTyped, Booleans: ini.BooleansExtended}},
}

// roundTrip checks that data, the INI that in was encoded to under cfg,
// decodes under cfg to a value equal to in. Since INI loses some detail, the
// comparison folds names when cfg folds them, reads scalars as the strings
// they were written as unless values are typed, and reads a one-element list
// as its element.
func roundTrip(t testing.TB, cfg ini.Config, in cue.Value, data []byte) {
	t.Helper()
	expr, err := ini.NewDecoder("out.ini", bytes.NewReader(data), cfg).Decode()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("decoding:\n%s", data))
	out := cuecontext.New().BuildExpr(expr)
	qt.Assert(t, qt.IsNil(out.Err()))
	qt.Assert(t, qt.DeepEquals(comparable(t, cfg, out), comparable(t, cfg, in)), qt.Commentf("decoding:\n%s", data))
}

// typedNumber is a number decoded or written under [ini.ValuesTyped], held
// as its JSON text so that a number and a string with the same text differ.
type typedNumber string

// comparable turns v into nested maps, slices, and scalars that compare
// equal exactly when the round trip of [roundTrip] preserved v.
func comparable(t testing.TB, cfg ini.Config, v cue.Value) any {
	t.Helper()
	typed := cfg.Values == ini.ValuesTyped
	switch v.Kind() {
	case cue.StructKind:
		m := map[string]any{}
		iter, err := v.Fields()
		qt.Assert(t, qt.IsNil(err))
		for iter.Next() {
			name := iter.Selector().Unquoted()
			if cfg.Case != 0 && cfg.Case != ini.CasePreserve {
				name = strings.ToLower(name)
			}
			m[name] = comparable(t, cfg, iter.Value())
		}
		return m
	case cue.ListKind:
		var list []any
		iter, err := v.List()
		qt.Assert(t, qt.IsNil(err))
		for iter.Next() {
			list = append(list, comparable(t, cfg, iter.Value()))
		}
		if len(list) == 1 {
			return list[0]
		}
		return list
	case cue.NullKind:
		return nil
	case cue.StringKind:
		s, err := v.String()
		qt.Assert(t, qt.IsNil(err))
		return s
	case cue.BoolKind:
		b, err := v.Bool()
		qt.Assert(t, qt.IsNil(err))
		if typed {
			return b
		}
		return strconv.FormatBool(b)
	case cue.IntKind, cue.FloatKind:
		text, err := v.MarshalJSON()
		qt.Assert(t, qt.IsNil(err))
		if typed {
			return typedNumber(text)
		}
		return string(text)
	}
	t.Fatalf("unexpected kind %v", v.Kind())
	return nil
}
