// Copyright 2019 CUE Authors
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

package literal

import (
	"fmt"
	"testing"
)

func TestUnquote(t *testing.T) {
	testCases := []struct {
		in, out string
		err     error
	}{
		{`"Hello"`, "Hello", nil},
		{`'Hello'`, "Hello", nil},
		{`'Hellø'`, "Hellø", nil},
		{`"""` + "\n\t\tHello\n\t\t" + `"""`, "Hello", nil},
		{"'''\n\t\tHello\n\t\t'''", "Hello", nil},
		{"'''\n\t\tHello\n\n\t\t'''", "Hello\n", nil},
		{"'''\n\n\t\tHello\n\t\t'''", "\nHello", nil},
		{"'''\n\n\n\n\t\t'''", "\n\n", nil},
		{"'''\n\t\t'''", "", nil},
		{`"""` + "\n\raaa\n\rbbb\n\r" + `"""`, "aaa\nbbb", nil},
		{`'\a\b\f\n\r\t\v\'\\\/'`, "\a\b\f\n\r\t\v'\\/", nil},
		{`"\a\b\f\n\r\t\v\"\\\/"`, "\a\b\f\n\r\t\v\"\\/", nil},
		{`#"The sequence "\U0001F604" renders as \#U0001F604."#`,
			`The sequence "\U0001F604" renders as 😄.`,
			nil},
		{`"  \U00010FfF"`, "  \U00010fff", nil},
		{`"\u0061 "`, "a ", nil},
		{`'\x61\x55'`, "\x61\x55", nil},
		{`'\061\055'`, "\061\055", nil},
		{`'\377 '`, "\377 ", nil},
		{"'e\u0300\\n'", "e\u0300\n", nil},
		{`'\06\055'`, "", errSyntax},
		{`'\0'`, "", errSyntax},
		{`"\06\055"`, "", errSyntax},    // too short
		{`'\777 '`, "", errSyntax},      // overflow
		{`'\U012301'`, "", errSyntax},   // too short
		{`'\U0123012G'`, "", errSyntax}, // invalid digit G
		{`"\x04"`, "", errSyntax},       // not allowed in strings
		{`'\U01230123'`, "", errSyntax}, // too large

		// Surrogate pairs
		{`"\uD834\uDD1E"`, "𝄞", nil},
		{`"\uDD1E\uD834"`, "", errSurrogate},
		{`"\uD834\uD834"`, "", errSurrogate},

		{`"\\"`, "\\", nil},
		{`"\'"`, "", errSyntax},
		{`"\q"`, "", errSyntax},
		{"'\n'", "", errSyntax},
		{"'---\n---'", "", errSyntax},
		{"'''\r'''", "", errMissingNewline},

		{`#"Hello"#`, "Hello", nil},
		{`#"Hello\v"#`, "Hello\\v", nil},
		{`#"Hello\#v\r"#`, "Hello\v\\r", nil},
		{`##"Hello\##v\r"##`, "Hello\v\\r", nil},
		{`##"Hello\##v"##`, "Hello\v", nil},
		{"#'''\n\t\tHello\\#v\n\t\t'''#", "Hello\v", nil},
		{"##'''\n\t\tHello\\#v\n\t\t'''##", "Hello\\#v", nil},
		{`#"""` + "\n\t\t\\#r\n\t\t" + `"""#`, "\r", nil},
		{`#""#`, "", nil},
		{`#"This is a "dog""#`, `This is a "dog"`, nil},
		{"#\"\"\"\n\"\n\"\"\"#", `"`, nil},
		{"#\"\"\"\n\"\"\"\n\"\"\"#", `"""`, nil},
		{"#\"\"\"\n\na\n\n\"\"\"#", "\na\n", nil},
		// Gobble extra \r
		{"#\"\"\"\n\ra\n\r\"\"\"#", `a`, nil},
		{"#\"\"\"\n\r\n\ra\n\r\n\r\"\"\"#", "\na\n", nil},
		// Make sure this works for Windows.
		{"#\"\"\"\r\n\r\na\r\n\r\n\"\"\"#", "\na\n", nil},
		{"#\"\"\"\r\n \r\n a\r\n \r\n \"\"\"#", "\na\n", nil},
		{"#\"\"\"\r\na\r\n\"\"\"#", `a`, nil},
		{"#\"\"\"\r\n\ra\r\n\r\"\"\"#", `a`, nil},
		{`####"   \"####`, `   \`, nil},

		{"```", "", errSyntax},
		{"Hello", "", errSyntax},
		{`"Hello`, "", errUnmatchedQuote},
		{`"""Hello"""`, "", errMissingNewline},
		{"'''\n  Hello\n   '''", "", errInvalidWhitespace},
		{"'''\n   a\n  b\n   '''", "", errInvalidWhitespace},
		{`"Hello""`, "", errSyntax},
		{`#"Hello"`, "", errUnmatchedQuote},
		{`#"Hello'#`, "", errUnmatchedQuote},
		{`#"""#`, "", errMissingNewline},

		// TODO: should these be legal?
		{`#"""#`, "", errMissingNewline},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d/%s", i, tc.in), func(t *testing.T) {
			if got, err := Unquote(tc.in); err != tc.err {
				t.Errorf("error: got %q; want %q", err, tc.err)
			} else if got != tc.out {
				t.Errorf("value: got %q; want %q", got, tc.out)
			}
		})
	}
}

func TestInterpolation(t *testing.T) {
	testCases := []struct {
		quotes string
		in     string
		out    string
		err    error
	}{
		{`""`, `foo\(`, "foo", nil},
		{`"""` + "\n" + `"""`, `foo`, "", errUnmatchedQuote},
		{`#""#`, `foo\#(`, "foo", nil},
		{`#""#`, `foo\(`, "", errUnmatchedQuote},
		{`""`, `foo\(bar`, "", errSyntax},
		{`""`, ``, "", errUnmatchedQuote},
		{`#""#`, `"`, "", errUnmatchedQuote},
		{`#""#`, `\`, "", errUnmatchedQuote},
		{`##""##`, `\'`, "", errUnmatchedQuote},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d/%s/%s", i, tc.quotes, tc.in), func(t *testing.T) {
			info, _, _, _ := ParseQuotes(tc.quotes, tc.quotes)
			if got, err := info.Unquote(tc.in); err != tc.err {
				t.Errorf("error: got %q; want %q", err, tc.err)
			} else if got != tc.out {
				t.Errorf("value: got %q; want %q", got, tc.out)
			}
		})
	}
}

func TestIsDouble(t *testing.T) {
	testCases := []struct {
		quotes string
		double bool
	}{
		{`""`, true},
		{`"""` + "\n" + `"""`, true},
		{`#""#`, true},
		{`''`, false},
		{`'''` + "\n" + `'''`, false},
		{`#''#`, false},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d/%s", i, tc.quotes), func(t *testing.T) {
			info, _, _, err := ParseQuotes(tc.quotes, tc.quotes)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.IsDouble(); got != tc.double {
				t.Errorf("got %v; want %v", got, tc.double)
			}
		})
	}
}
