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

// This file implements a parser test harness. The files in the testdata
// directory are parsed and the errors reported are compared against the
// error messages expected in the test files. The test files must end in
// .src rather than .go so that they are not disturbed by gofmt runs.
//
// Expected errors are indicated in the test files by putting a comment
// of the form /* ERROR "rx" */ immediately following an offending
// The harness will verify that an error matching the regular expression
// rx is reported at that source position.
//
// For instance, the following test file indicates that a "not declared"
// error should be reported for the undeclared variable x:
//
//	package p
//	{
//		a = x /* ERROR "not declared" */ + 1
//	}

package parser

import (
	"regexp"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/scanner"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/source"
)

const testdata = "testdata"

func getPos(f *token.File, offset int) token.Pos {
	if f != nil {
		return f.Pos(offset, 0)
	}
	return token.NoPos
}

// ERROR comments must be of the form /* ERROR "rx" */ and rx is
// a regular expression that matches the expected error message.
// The special form /* ERROR HERE "rx" */ must be used for error
// messages that appear immediately after a token, rather than at
// a token's position.
//
var errRx = regexp.MustCompile(`^/\* *ERROR *(HERE)? *"([^"]*)" *\*/$`)

// expectedErrors collects the regular expressions of ERROR comments found
// in files and returns them as a map of error positions to error messages.
//
func expectedErrors(t *testing.T, file *token.File, src []byte) map[token.Pos]string {
	errors := make(map[token.Pos]string)

	var s scanner.Scanner
	// file was parsed already - do not add it again to the file
	// set otherwise the position information returned here will
	// not match the position information collected by the parser
	// file := token.NewFile(filename, -1, len(src))
	s.Init(file, src, nil, scanner.ScanComments)
	var prev token.Pos // position of last non-comment, non-semicolon token
	var here token.Pos // position immediately after the token at position prev

	for {
		pos, tok, lit := s.Scan()
		pos = pos.WithRel(0)
		switch tok {
		case token.EOF:
			return errors
		case token.COMMENT:
			s := errRx.FindStringSubmatch(lit)
			if len(s) == 3 {
				pos := prev
				if s[1] == "HERE" {
					pos = here
				}
				errors[pos] = string(s[2])
			}
		default:
			prev = pos
			var l int // token length
			if tok.IsLiteral() {
				l = len(lit)
			} else {
				l = len(tok.String())
			}
			here = prev.Add(l)
		}
	}
}

// compareErrors compares the map of expected error messages with the list
// of found errors and reports discrepancies.
//
func compareErrors(t *testing.T, file *token.File, expected map[token.Pos]string, found []errors.Error) {
	t.Helper()
	for _, error := range found {
		// error.Pos is a Position, but we want
		// a Pos so we can do a map lookup
		ePos := error.Position()
		eMsg := error.Error()
		pos := getPos(file, ePos.Offset()).WithRel(0)
		if msg, found := expected[pos]; found {
			// we expect a message at pos; check if it matches
			rx, err := regexp.Compile(msg)
			if err != nil {
				t.Errorf("%s: %v", ePos, err)
				continue
			}
			if match := rx.MatchString(eMsg); !match {
				t.Errorf("%s: %q does not match %q", ePos, eMsg, msg)
				continue
			}
			// we have a match - eliminate this error
			delete(expected, pos)
		} else {
			// To keep in mind when analyzing failed test output:
			// If the same error position occurs multiple times in errors,
			// this message will be triggered (because the first error at
			// the position removes this position from the expected errors).
			t.Errorf("%s: unexpected error: -%q-", ePos, eMsg)
		}
	}

	// there should be no expected errors left
	if len(expected) > 0 {
		t.Errorf("%d errors not reported:", len(expected))
		for pos, msg := range expected {
			t.Errorf("%s: -%q-\n", pos, msg)
		}
	}
}

func checkErrors(t *testing.T, filename string, input interface{}) {
	t.Helper()
	src, err := source.Read(filename, input)
	if err != nil {
		t.Error(err)
		return
	}

	f, err := ParseFile(filename, src, DeclarationErrors, AllErrors)
	file := f.Pos().File()
	found := errors.Errors(err)

	// we are expecting the following errors
	// (collect these after parsing a file so that it is found in the file set)
	if file == nil {
		t.Fatal("")
	}
	expected := expectedErrors(t, file, src)

	// verify errors returned by the parser
	compareErrors(t, file, expected, found)
}

func TestFuzz(t *testing.T) {
	testCases := []string{
		"(({\"\\(0)\"(",
		"{{\"\\(0\xbf\"(",
		"a:y for x n{b:\"\"(\"\\(" +
			"\"\"\\\"(",
	}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			_, _ = ParseFile("go-fuzz", []byte(tc))
		})
	}
}
