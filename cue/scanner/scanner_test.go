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

package scanner

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

const /* class */ (
	special = iota
	literal
	operator
	keyword
)

func tokenclass(tok token.Token) int {
	switch {
	case tok.IsLiteral():
		return literal
	case tok.IsOperator():
		return operator
	case tok.IsKeyword():
		return keyword
	}
	return special
}

type elt struct {
	tok   token.Token
	lit   string
	class int
	want  string // expected scanned literal, if different from lit (e.g. after \r stripping)
}

var testTokens = [...]elt{
	// Special tokens
	{tok: token.COMMENT, lit: "// a comment \n", class: special},
	{tok: token.COMMENT, lit: "//\r\n", class: special, want: "//"},

	// Attributes
	{tok: token.ATTRIBUTE, lit: "@foo()", class: special},
	{tok: token.ATTRIBUTE, lit: "@foo(,,)", class: special},
	{tok: token.ATTRIBUTE, lit: "@foo(a)", class: special},
	{tok: token.ATTRIBUTE, lit: "@foo(aa=b)", class: special},
	{tok: token.ATTRIBUTE, lit: "@foo(,a=b)", class: special},
	{tok: token.ATTRIBUTE, lit: `@foo(",a=b")`, class: special},
	{tok: token.ATTRIBUTE, lit: `@foo(##"\(),a=b"##)`, class: special},
	{tok: token.ATTRIBUTE, lit: `@foo("",a="")`, class: special},
	{tok: token.ATTRIBUTE, lit: `@foo(2,bytes,a.b=c)`, class: special},
	{tok: token.ATTRIBUTE, lit: `@foo([{()}]())`, class: special},
	{tok: token.ATTRIBUTE, lit: `@foo("{")`, class: special},

	// Identifiers and basic type literals
	{tok: token.BOTTOM, lit: "_|_", class: literal},

	{tok: token.IDENT, lit: "foobar", class: literal},
	{tok: token.IDENT, lit: "$foobar", class: literal},
	{tok: token.IDENT, lit: "#foobar", class: literal},
	// {tok: token.IDENT, lit: "#0", class: literal},
	{tok: token.IDENT, lit: "#", class: literal},
	{tok: token.IDENT, lit: "_foobar", class: literal},
	{tok: token.IDENT, lit: "__foobar", class: literal},
	{tok: token.IDENT, lit: "#_foobar", class: literal},
	{tok: token.IDENT, lit: "_#foobar", class: literal},
	{tok: token.IDENT, lit: "a۰۱۸", class: literal},
	{tok: token.IDENT, lit: "foo६४", class: literal},
	{tok: token.IDENT, lit: "bar９８７６", class: literal},
	{tok: token.IDENT, lit: "ŝ", class: literal},
	{tok: token.IDENT, lit: "ŝfoo", class: literal},
	{tok: token.INT, lit: "0", class: literal},
	{tok: token.INT, lit: "1", class: literal},
	{tok: token.INT, lit: "123456789012345678890", class: literal},
	{tok: token.INT, lit: "12345_67890_12345_6788_90", class: literal},
	{tok: token.INT, lit: "1234567M", class: literal},
	{tok: token.INT, lit: "1234567Mi", class: literal},
	{tok: token.INT, lit: "1234567", class: literal},
	{tok: token.INT, lit: ".3Mi", class: literal},
	{tok: token.INT, lit: "3.3Mi", class: literal},
	{tok: token.INT, lit: "0xcafebabe", class: literal},
	{tok: token.INT, lit: "0b1100_1001", class: literal},
	{tok: token.INT, lit: "0o1234567", class: literal},
	{tok: token.FLOAT, lit: "0.", class: literal},
	{tok: token.FLOAT, lit: ".0", class: literal},
	{tok: token.FLOAT, lit: "3.14159265", class: literal},
	{tok: token.FLOAT, lit: "1e0", class: literal},
	{tok: token.FLOAT, lit: "1e+100", class: literal},
	{tok: token.FLOAT, lit: "1e-100", class: literal},
	{tok: token.FLOAT, lit: "1E+100", class: literal},
	{tok: token.FLOAT, lit: "1E-100", class: literal},
	{tok: token.FLOAT, lit: "0e-5", class: literal},
	{tok: token.FLOAT, lit: "0e+100", class: literal},
	{tok: token.FLOAT, lit: "0e-100", class: literal},
	{tok: token.FLOAT, lit: "0E+100", class: literal},
	{tok: token.FLOAT, lit: "0E-100", class: literal},
	{tok: token.FLOAT, lit: "2.71828e-1000", class: literal},
	{tok: token.STRING, lit: "'a'", class: literal},
	{tok: token.STRING, lit: "'\\000'", class: literal},
	{tok: token.STRING, lit: "'\\xFF'", class: literal},
	{tok: token.STRING, lit: "'\\uff16'", class: literal},
	{tok: token.STRING, lit: "'\\uD801'", class: literal},
	{tok: token.STRING, lit: "'\\U0000ff16'", class: literal},
	{tok: token.STRING, lit: "'foobar'", class: literal},
	{tok: token.STRING, lit: `'foo\/bar'`, class: literal},
	{tok: token.STRING, lit: `#" ""#`, class: literal},
	{tok: token.STRING, lit: `#"" "#`, class: literal},
	{tok: token.STRING, lit: `#""hello""#`, class: literal},
	{tok: token.STRING, lit: `##""# "##`, class: literal},
	{tok: token.STRING, lit: `####""###"####`, class: literal},
	{tok: token.STRING, lit: "##\"\"\"\n\"\"\"#\n\"\"\"##", class: literal},
	{tok: token.STRING, lit: `##"####"##`, class: literal},
	{tok: token.STRING, lit: `#"foobar"#`, class: literal},
	{tok: token.STRING, lit: `#" """#`, class: literal},
	{tok: token.STRING, lit: `#"\r"#`, class: literal},
	{tok: token.STRING, lit: `#"\("#`, class: literal},
	{tok: token.STRING, lit: `#"\q"#`, class: literal},
	{tok: token.STRING, lit: `###"\##q"###`, class: literal},
	{tok: token.STRING, lit: "'" + `\r` + "'", class: literal},
	{tok: token.STRING, lit: "'foo" + `\r\n` + "bar'", class: literal},
	{tok: token.STRING, lit: `"foobar"`, class: literal},
	{tok: token.STRING, lit: "\"\"\"\n  foobar\n  \"\"\"", class: literal},
	{tok: token.STRING, lit: "#\"\"\"\n  \\(foobar\n  \"\"\"#", class: literal},
	// TODO: should we preserve the \r instead and have it removed by the
	// literal parser? This would allow preserving \r for formatting without
	// changing the semantics of evaluation.
	{tok: token.STRING, lit: "#\"\"\"\r\n  \\(foobar\n  \"\"\"#", class: literal},
	// Bare \r in multiline strings should be stripped and not prevent
	// the closing quotes from being recognized.
	{tok: token.STRING, lit: "\"\"\"\n\r\"\"\"", class: literal, want: "\"\"\"\n\"\"\""},
	{tok: token.STRING, lit: "\"\"\"\n\r\n\r\"\"\"", class: literal, want: "\"\"\"\n\n\"\"\""},
	{tok: token.STRING, lit: "\"\"\"\n\rfoo\n\r\"\"\"", class: literal, want: "\"\"\"\nfoo\n\"\"\""},

	// Operators and delimiters
	{tok: token.ADD, lit: "+", class: operator},
	{tok: token.SUB, lit: "-", class: operator},
	{tok: token.MUL, lit: "*", class: operator},
	{tok: token.QUO, lit: "/", class: operator},

	{tok: token.AND, lit: "&", class: operator},
	{tok: token.OR, lit: "|", class: operator},

	{tok: token.LAND, lit: "&&", class: operator},
	{tok: token.LOR, lit: "||", class: operator},

	{tok: token.EQL, lit: "==", class: operator},
	{tok: token.LSS, lit: "<", class: operator},
	{tok: token.GTR, lit: ">", class: operator},
	{tok: token.BIND, lit: "=", class: operator},
	{tok: token.NOT, lit: "!", class: operator},

	{tok: token.NEQ, lit: "!=", class: operator},
	{tok: token.LEQ, lit: "<=", class: operator},
	{tok: token.GEQ, lit: ">=", class: operator},
	{tok: token.ELLIPSIS, lit: "...", class: operator},

	{tok: token.MAT, lit: "=~", class: operator},
	{tok: token.NMAT, lit: "!~", class: operator},

	{tok: token.LPAREN, lit: "(", class: operator},
	{tok: token.LBRACK, lit: "[", class: operator},
	{tok: token.LBRACE, lit: "{", class: operator},
	{tok: token.COMMA, lit: ",", class: operator},
	{tok: token.PERIOD, lit: ".", class: operator},
	{tok: token.OPTION, lit: "?", class: operator},

	{tok: token.RPAREN, lit: ")", class: operator},
	{tok: token.RBRACK, lit: "]", class: operator},
	{tok: token.RBRACE, lit: "}", class: operator},
	{tok: token.COLON, lit: ":", class: operator},

	// Keywords
	{tok: token.TRUE, lit: "true", class: keyword},
	{tok: token.FALSE, lit: "false", class: keyword},
	{tok: token.NULL, lit: "null", class: keyword},

	{tok: token.FOR, lit: "for", class: keyword},
	{tok: token.IF, lit: "if", class: keyword},
	{tok: token.IN, lit: "in", class: keyword},
}

const whitespace = "  \t  \n\n\n" // to separate tokens

var source = func() []byte {
	var src []byte
	for _, t := range testTokens {
		src = append(src, t.lit...)
		src = append(src, whitespace...)
	}
	return src
}()

func newlineCount(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}

func checkPosScan(t *testing.T, lit string, p token.Pos, expected token.Position) {
	pos := p.Position()
	if pos.Filename != expected.Filename {
		t.Errorf("bad filename for %q: got %s, expected %s", lit, pos.Filename, expected.Filename)
	}
	if pos.Offset != expected.Offset {
		t.Errorf("bad position for %q: got %d, expected %d", lit, pos.Offset, expected.Offset)
	}
	if pos.Line != expected.Line {
		t.Errorf("bad line for %q: got %d, expected %d", lit, pos.Line, expected.Line)
	}
	if pos.Column != expected.Column {
		t.Errorf("bad column for %q: got %d, expected %d", lit, pos.Column, expected.Column)
	}
}

// Verify that calling Scan() provides the correct results.
func TestScan(t *testing.T) {
	whitespace_linecount := newlineCount(whitespace)

	// error handler
	eh := func(_ token.Pos, msg string, args []interface{}) {
		t.Errorf("error handler called (msg = %s)", fmt.Sprintf(msg, args...))
	}

	// verify scan
	var s Scanner
	s.Init(token.NewFile("", -1, len(source)), source, eh, ScanComments|DontInsertCommas)

	// set up expected position
	epos := token.Position{
		Filename: "",
		Offset:   0,
		Line:     1,
		Column:   1,
	}

	index := 0
	for {
		pos, tok, lit := s.Scan()

		// check position
		if tok == token.EOF {
			// correction for EOF
			epos.Line = newlineCount(string(source))
			epos.Column = 2
		}
		checkPosScan(t, lit, pos, epos)

		// check token
		e := elt{tok: token.EOF, class: special}
		if index < len(testTokens) {
			e = testTokens[index]
			index++
		}
		if tok != e.tok {
			t.Errorf("bad token for %q: got %s, expected %s", lit, tok, e.tok)
		}

		// check token class
		if tokenclass(tok) != e.class {
			t.Errorf("bad class for %q: got %d, expected %d", lit, tokenclass(tok), e.class)
		}

		// check literal
		elit := ""
		if e.want != "" {
			elit = e.want
		} else {
			switch e.tok {
			case token.COMMENT:
				elit = e.lit
				//-style comment literal doesn't contain newline
				if elit[1] == '/' {
					elit = elit[0 : len(elit)-1]
				}
			case token.ATTRIBUTE:
				elit = e.lit
			case token.IDENT:
				elit = e.lit
			case token.COMMA:
				elit = ","
			default:
				if e.tok.IsLiteral() {
					elit = e.lit
				} else if e.tok.IsKeyword() {
					elit = e.lit
				}
			}
		}
		if lit != elit {
			t.Errorf("bad literal for %q: got %q, expected %q", lit, lit, elit)
		}

		if tok == token.EOF {
			break
		}

		// update position
		epos.Offset += len(e.lit) + len(whitespace)
		epos.Line += newlineCount(e.lit) + whitespace_linecount

	}

	if s.ErrorCount != 0 {
		t.Errorf("found %d errors", s.ErrorCount)
	}
}

func checkComma(t *testing.T, line string, mode Mode) {
	var S Scanner
	file := token.NewFile("TestCommas", -1, len(line))
	S.Init(file, []byte(line), nil, mode)
	pos, tok, lit := S.Scan()
	for tok != token.EOF {
		if tok == token.ILLEGAL {
			// the illegal token literal indicates what
			// kind of semicolon literal to expect
			commaLit := "\n"
			if lit[0] == '%' {
				commaLit = ","
			}
			// next token must be a comma
			commaPos := file.Position(pos)
			commaPos.Offset++
			commaPos.Column++
			pos, tok, lit = S.Scan()
			if tok == token.COMMA {
				if lit != commaLit {
					t.Errorf(`bad literal for %q: got %q (%q), expected %q`, line, lit, tok, commaLit)
				}
				checkPosScan(t, line, pos, commaPos)
			} else {
				t.Errorf("bad token for %q: got %s, expected ','", line, tok)
			}
		} else if tok == token.COMMA {
			t.Errorf("bad token for %q: got ',', expected no ','", line)
		}
		pos, tok, lit = S.Scan()
	}
}

var lines = []string{
	// % indicates a comma present in the source
	// ^ indicates an automatically inserted comma
	"",
	"\ufeff%,", // first BOM is ignored
	"%,",
	"foo^\n",
	"_foo^\n",
	"123^\n",
	"1.2^\n",
	"'x'^\n",
	"_|_^\n",
	"_|_^\n",
	`"x"` + "^\n",
	"#'x'#^\n",
	`"""
		foo
		"""` + "^\n",
	// `"""
	// 	foo \(bar)
	// 	"""` + "^\n",
	`'''
		foo
		'''` + "^\n",

	"+\n",
	"-\n",
	"*\n",
	"/\n",

	"&\n",
	// "&^\n",
	"|\n",

	"&&\n",
	"||\n",
	"<-\n",
	"->\n",

	"==\n",
	"<\n",
	">\n",
	"=\n",
	"!\n",

	"!=\n",
	"<=\n",
	">=\n",
	":=\n",
	"...^\n",

	"(\n",
	"[\n",
	"[[\n",
	"{\n",
	"{{\n",
	"%,\n",
	".\n",

	")^\n",
	"]^\n",
	"]]^\n",
	"}^\n",
	"}}^\n",
	":\n",
	"::\n",
	";^\n",

	"true^\n",
	"false^\n",
	"null^\n",

	"foo^//comment\n",
	"foo^//comment",

	"foo    ^// comment\n",
	"foo    ^// comment",

	"foo    ^",
	"foo    ^//",

	"package main^\n\nfoo: bar^",
	"package main^",
}

func TestCommas(t *testing.T) {
	for _, line := range lines {
		checkComma(t, line, 0)
		checkComma(t, line, ScanComments)

		// if the input ended in newlines, the input must tokenize the
		// same with or without those newlines
		for i := len(line) - 1; i >= 0 && line[i] == '\n'; i-- {
			checkComma(t, line[0:i], 0)
			checkComma(t, line[0:i], ScanComments)
		}
	}
}

func TestRelative(t *testing.T) {
	test := `
	package foo

	// comment
	a: 1 // a
	b :    5
	// line one
	// line two
	c
	: "dfs"
	, d: "foo"
	`
	want := []string{
		`newline IDENT    package`,
		`blank   IDENT    foo`,
		"elided  ,        \n",
		`section COMMENT  // comment`,
		`newline IDENT    a`,
		`nospace :        `,
		`blank   INT      1`,
		"elided  ,        \n",
		`blank   COMMENT  // a`,
		`newline IDENT    b`,
		`blank   :        `,
		`blank   INT      5`,
		"elided  ,        \n",
		"newline COMMENT  // line one",
		"newline COMMENT  // line two",
		`newline IDENT    c`,
		`newline :        `,
		`blank   STRING   "dfs"`,
		"newline ,        ,",
		"blank   IDENT    d",
		"nospace :        ",
		`blank   STRING   "foo"`,
		"elided  ,        \n",
	}
	var S Scanner
	f := token.NewFile("TestCommas", -1, len(test))
	S.Init(f, []byte(test), nil, ScanComments)
	pos, tok, lit := S.Scan()
	got := []string{}
	for tok != token.EOF {
		got = append(got, fmt.Sprintf("%-7s %-8s %s", pos.RelPos(), tok, lit))
		pos, tok, lit = S.Scan()
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Error(diff)
	}
}

// Verify that initializing the same scanner more than once works correctly.
func TestInit(t *testing.T) {
	var s Scanner

	// 1st init
	src1 := "false true { }"
	f1 := token.NewFile("src1", -1, len(src1))
	s.Init(f1, []byte(src1), nil, DontInsertCommas)
	if f1.Size() != len(src1) {
		t.Errorf("bad file size: got %d, expected %d", f1.Size(), len(src1))
	}
	s.Scan()              // false
	s.Scan()              // true
	_, tok, _ := s.Scan() // {
	if tok != token.LBRACE {
		t.Errorf("bad token: got %s, expected %s", tok, token.LBRACE)
	}

	// 2nd init
	src2 := "null true { ]"
	f2 := token.NewFile("src2", -1, len(src2))
	s.Init(f2, []byte(src2), nil, DontInsertCommas)
	if f2.Size() != len(src2) {
		t.Errorf("bad file size: got %d, expected %d", f2.Size(), len(src2))
	}
	_, tok, _ = s.Scan() // go
	if tok != token.NULL {
		t.Errorf("bad token: got %s, expected %s", tok, token.NULL)
	}

	if s.ErrorCount != 0 {
		t.Errorf("found %d errors", s.ErrorCount)
	}
}

func TestScanInterpolation(t *testing.T) {
	// error handler
	eh := func(pos token.Pos, msg string, args []interface{}) {
		msg = fmt.Sprintf(msg, args...)
		t.Errorf("error handler called (pos = %v, msg = %s)", pos, msg)
	}
	trim := func(s string) string { return strings.Trim(s, `#"\()`) }

	sources := []string{
		`"first\(first)\\second\(second)"`,
		`#"first\#(first)\second\#(second)"#`,
		`"level\( ["foo", "level", level ][2] )end\( end )"`,
		`##"level\##( ["foo", "level", level ][2] )end\##( end )"##`,
		`"level\( { "foo": 1, "bar": level } )end\(end)"`,
	}
	for i, src := range sources {
		name := fmt.Sprintf("tsrc%d", i)
		t.Run(name, func(t *testing.T) {
			f := token.NewFile(name, -1, len(src))

			// verify scan
			var s Scanner
			s.Init(f, []byte(src), eh, ScanComments)

			count := 0
			var lit, str string
			for tok := token.ILLEGAL; tok != token.EOF; {
				switch tok {
				case token.LPAREN:
					count++
				case token.RPAREN:
					if count--; count == 0 {
						str = trim(s.ResumeInterpolation())
					}
				case token.INTERPOLATION:
					str = trim(lit)
				case token.IDENT:
					if lit != str {
						t.Errorf("str: got %v; want %v", lit, str)
					}
				}
				_, tok, lit = s.Scan()
			}
		})
	}
}

func TestStdErrorHander(t *testing.T) {
	const src = "%\n" + // illegal character, cause an error
		"% %\n" + // two errors on the same line
		"//line File2:20\n" +
		"%\n" + // different file, but same line
		"//line File2:1\n" +
		"% %\n" + // same file, decreasing line number
		"//line File1:1\n" +
		"% % %" // original file, line 1 again

	var list errors.Error
	eh := func(pos token.Pos, msg string, args []interface{}) {
		list = errors.Append(list, errors.Newf(pos, msg, args...))
	}

	var s Scanner
	s.Init(token.NewFile("File1", -1, len(src)), []byte(src), eh, DontInsertCommas)
	for {
		if _, tok, _ := s.Scan(); tok == token.EOF {
			break
		}
	}

	n := len(errors.Errors(list))
	if n != s.ErrorCount {
		t.Errorf("found %d errors, expected %d", n, s.ErrorCount)
	}

	if n != 9 {
		t.Errorf("found %d raw errors, expected 9", n)
		errors.Print(os.Stderr, list, nil)
	}

	// Note that this is 9 errors when sanitized, and not 8,
	// as we currently don't support //line comment directives.
	n = len(errors.Errors(errors.Sanitize(list)))
	if n != 9 {
		t.Errorf("found %d one-per-line errors, expected 9", n)
		errors.Print(os.Stderr, list, nil)
	}
}

type errorCollector struct {
	cnt int       // number of errors encountered
	msg string    // last error message encountered
	pos token.Pos // last error position encountered
}

func checkError(t *testing.T, src string, tok token.Token, pos int, lit, err string) {
	t.Helper()
	var s Scanner
	var h errorCollector
	eh := func(pos token.Pos, msg string, args []interface{}) {
		h.cnt++
		h.msg = fmt.Sprintf(msg, args...)
		h.pos = pos
	}
	s.Init(token.NewFile("", -1, len(src)), []byte(src), eh, ScanComments|DontInsertCommas)
	_, tok0, lit0 := s.Scan()
	// Scan the full input so that errors produced during string
	// continuation after interpolation are collected too.
	count := 0
	for scanTok := tok0; scanTok != token.EOF; {
		_, scanTok, _ = s.Scan()
		switch scanTok {
		case token.LPAREN:
			count++
		case token.RPAREN:
			if count--; count == 0 {
				s.ResumeInterpolation()
			}
		}
	}
	if tok0 != tok {
		t.Errorf("%q: got %s, expected %s", src, tok0, tok)
	}
	if tok0 != token.ILLEGAL && lit0 != lit {
		t.Errorf("%q: got literal %q, expected %q", src, lit0, lit)
	}
	cnt := 0
	if err != "" {
		cnt = 1
	}
	if h.cnt != cnt {
		t.Errorf("%q: got cnt %d, expected %d", src, h.cnt, cnt)
	}
	if h.msg != err {
		t.Errorf("%q: got msg %q, expected %q", src, h.msg, err)
	}
	if h.pos.Offset() != pos {
		t.Errorf("%q: got offset %d, expected %d", src, h.pos.Offset(), pos)
	}
}

var errorTests = []struct {
	src string
	tok token.Token
	pos int
	lit string
	err string
}{
	{"`", token.ILLEGAL, 0, "", "illegal character U+0060 '`'"},

	{"\a", token.ILLEGAL, 0, "", "illegal character U+0007"},
	{`^`, token.ILLEGAL, 0, "", "illegal character U+005E '^'"},
	{`…`, token.ILLEGAL, 0, "", "illegal character U+2026 '…'"},
	{`__#foobar`, token.ILLEGAL, 0, "", `illegal token "__#foobar"`},

	{`@`, token.ATTRIBUTE, 1, `@`, "invalid attribute: expected '('"},
	{`@foo`, token.ATTRIBUTE, 4, `@foo`, "invalid attribute: expected '('"},
	{`@foo(`, token.ATTRIBUTE, 5, `@foo(`, "attribute missing ')'"},
	{`@foo( `, token.ATTRIBUTE, 6, `@foo( `, "attribute missing ')'"},
	{`@foo( ""])`, token.ATTRIBUTE, 9, `@foo( ""])`, "unexpected ']'"},
	{`@foo(3})`, token.ATTRIBUTE, 7, `@foo(3})`, "unexpected '}'"},
	{`@foo(["")])`, token.ATTRIBUTE, 9, `@foo(["")])`, "unexpected ')'"},
	{`@foo(""`, token.ATTRIBUTE, 7, `@foo(""`, "attribute missing ')'"},
	{`@foo(aa`, token.ATTRIBUTE, 7, `@foo(aa`, "attribute missing ')'"},
	{`@foo("\(())")`, token.ATTRIBUTE, 7, `@foo("\(())")`, "interpolation not allowed in attribute"},

	// {`' '`, STRING, 0, `' '`, ""},
	// {"`\0`", STRING, 3, `'\0'`, "illegal character U+0027 ''' in escape sequence"},
	// {`'\07'`, STRING, 4, `'\07'`, "illegal character U+0027 ''' in escape sequence"},
	{`"\8"`, token.STRING, 2, `"\8"`, "unknown escape sequence"},
	{`"\08"`, token.STRING, 2, `"\08"`, "octal escape not allowed in string literal"},
	{`'\08'`, token.STRING, 3, `'\08'`, "illegal character U+0038 '8' in escape sequence"},
	{`"\x0"`, token.STRING, 2, `"\x0"`, "hexadecimal escape not allowed in string literal"},
	{`'\x0'`, token.STRING, 4, `'\x0'`, "illegal character U+0027 ''' in escape sequence"},
	{`"\x0g"`, token.STRING, 2, `"\x0g"`, "hexadecimal escape not allowed in string literal"},
	{`'\x0g'`, token.STRING, 4, `'\x0g'`, "illegal character U+0067 'g' in escape sequence"},
	{`"\x"`, token.STRING, 2, `"\x"`, "hexadecimal escape not allowed in string literal"},
	{`'\x'`, token.STRING, 3, `'\x'`, "illegal character U+0027 ''' in escape sequence"},
	{`'\00_'`, token.STRING, 4, `'\00_'`, "illegal character U+005F '_' in escape sequence"},
	{`'\x0_'`, token.STRING, 4, `'\x0_'`, "illegal character U+005F '_' in escape sequence"},
	{`"\u00_0"`, token.STRING, 5, `"\u00_0"`, "illegal character U+005F '_' in escape sequence"},
	{`"\U000_0000"`, token.STRING, 6, `"\U000_0000"`, "illegal character U+005F '_' in escape sequence"},
	{`"\u"`, token.STRING, 3, `"\u"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\u0"`, token.STRING, 4, `"\u0"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\u00"`, token.STRING, 5, `"\u00"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\u000"`, token.STRING, 6, `"\u000"`, "illegal character U+0022 '\"' in escape sequence"},
	// {`"\u000`, token.STRING, 6, `"\u000`, "string literal not terminated"}, two errors
	{`"\u0000"`, token.STRING, 0, `"\u0000"`, ""},
	{`"\U"`, token.STRING, 3, `"\U"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U0"`, token.STRING, 4, `"\U0"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U00"`, token.STRING, 5, `"\U00"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U000"`, token.STRING, 6, `"\U000"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U0000"`, token.STRING, 7, `"\U0000"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U00000"`, token.STRING, 8, `"\U00000"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U000000"`, token.STRING, 9, `"\U000000"`, "illegal character U+0022 '\"' in escape sequence"},
	{`"\U0000000"`, token.STRING, 10, `"\U0000000"`, "illegal character U+0022 '\"' in escape sequence"},
	// {`"\U0000000`, token.STRING, 10, `"\U0000000`, "string literal not terminated"}, // escape sequence not terminated"}, two errors
	{`"\U00000000"`, token.STRING, 0, `"\U00000000"`, ""},
	{`"\Uffffffff"`, token.STRING, 2, `"\Uffffffff"`, "escape sequence is invalid Unicode code point"},
	{`'`, token.STRING, 0, `'`, "string literal not terminated"},
	{`"`, token.STRING, 0, `"`, "string literal not terminated"},
	{`""`, token.STRING, 0, `""`, ""},
	{`"abc`, token.STRING, 0, `"abc`, "string literal not terminated"},
	{`""abc`, token.STRING, 0, `""`, ""},
	{"\"\"\"\nabc", token.STRING, 0, "\"\"\"\nabc", "string literal not terminated"},
	{"\"\"\"\n0\"\"\"", token.STRING, 0, "\"\"\"\n0\"\"\"", "string literal not terminated"},
	{"'''\nabc", token.STRING, 0, "'''\nabc", "string literal not terminated"},
	{"'''\n0'''", token.STRING, 0, "'''\n0'''", "string literal not terminated"},
	{"\"\"\"\n0\n  \"\"\"", token.STRING, 0, "\"\"\"\n0\n  \"\"\"", "non-matching whitespace for multiline strings"},
	{"'''\n0\n  '''", token.STRING, 0, "'''\n0\n  '''", "non-matching whitespace for multiline strings"},
	{"\"\"\"\n\\(0)\n  \"\"\"", token.INTERPOLATION, 0, "\"\"\"\n\\(", "non-matching whitespace for multiline strings"},
	// Whitespace-only content lines must also match the closing whitespace.
	{"'''\n \n\t'''", token.STRING, 0, "'''\n \n\t'''", "non-matching whitespace for multiline strings"},
	{"\"\"\"\n \n\t\"\"\"", token.STRING, 0, "\"\"\"\n \n\t\"\"\"", "non-matching whitespace for multiline strings"},
	{"'''\n\t\n '''", token.STRING, 0, "'''\n\t\n '''", "non-matching whitespace for multiline strings"},
	// Content lines with same-length but incompatible whitespace prefixes.
	{"'''\n\t\n 0\n\t'''", token.STRING, 0, "'''\n\t\n 0\n\t'''", "non-matching whitespace for multiline strings"},
	{"'''\n \n\t0\n '''", token.STRING, 0, "'''\n \n\t0\n '''", "non-matching whitespace for multiline strings"},
	// Longer shared prefix that diverges (tab vs space after common prefix).
	{"'''\n\t\t 0\n\t\t\t0\n\t\t\t'''", token.STRING, 0, "'''\n\t\t 0\n\t\t\t0\n\t\t\t'''", "non-matching whitespace for multiline strings"},
	{"\"abc\n", token.STRING, 0, `"abc`, "string literal not terminated"},
	{"\"abc\n   ", token.STRING, 0, `"abc`, "string literal not terminated"},
	{"\"abc\r\n   ", token.STRING, 0, "\"abc\r", "string literal not terminated"},
	{`#""`, token.STRING, 0, `#""`, "string literal not terminated"},
	{`#"""`, token.STRING, 0, `#"""`, `expected newline after multiline quote #"""`},
	{`#""#`, token.STRING, 0, `#""#`, ""},
	{"$", token.IDENT, 0, "$", ""},
	{"#'", token.STRING, 0, "#'", "string literal not terminated"},
	{"''", token.STRING, 0, "''", ""},
	{"'", token.STRING, 0, "'", "string literal not terminated"},
	{`"\(0)"`, token.INTERPOLATION, 0, `"\(`, ""},
	{`#"\("#`, token.STRING, 0, `#"\("#`, ""},
	{`#"\#(0)"#`, token.INTERPOLATION, 0, `#"\#(`, ""},
	{`"\q"`, token.STRING, 2, `"\q"`, "unknown escape sequence"},
	{`#"\q"#`, token.STRING, 0, `#"\q"#`, ""},
	{`#"\#q"#`, token.STRING, 4, `#"\#q"#`, "unknown escape sequence"},
	{"0", token.INT, 0, "0", ""},
	{"00", token.INT, 0, "00", "illegal integer number"},
	{"00...", token.INT, 0, "00", "illegal integer number"},
	{"077", token.INT, 0, "077", "illegal integer number"},
	{"077...", token.INT, 0, "077", "illegal integer number"},
	{"078.", token.FLOAT, 0, "078.", ""},
	{"07801234567.", token.FLOAT, 0, "07801234567.", ""},
	{"078e0", token.FLOAT, 0, "078e0", ""},
	{"078", token.INT, 0, "078", "illegal integer number"},
	{"078...", token.INT, 0, "078", "illegal integer number"},
	{"07800000009", token.INT, 0, "07800000009", "illegal integer number"},
	{"0x", token.INT, 0, "0x", "illegal hexadecimal number"},
	{"0X", token.INT, 0, "0X", "illegal hexadecimal number"},
	{"0Xbeef_", token.INT, 6, "0Xbeef_", "illegal '_' in number"},
	{"0Xbeef__beef", token.INT, 7, "0Xbeef__beef", "illegal '_' in number"},
	{"0b", token.INT, 0, "0b", "illegal binary number"},
	{"0o", token.INT, 0, "0o", "illegal octal number"},
	{"0E", token.FLOAT, 2, "0E", "illegal exponent in number"},
	{"0e+", token.FLOAT, 3, "0e+", "illegal exponent in number"},
	{"0E-", token.FLOAT, 3, "0E-", "illegal exponent in number"},
	{"1.5e", token.FLOAT, 4, "1.5e", "illegal exponent in number"},
	{"0ea", token.FLOAT, 2, "0e", "illegal exponent in number"},
	// {"123456789012345678890_i", IMAG, 21, "123456789012345678890_i", "illegal '_' in number"},
	{"\"abc\x00def\"", token.STRING, 4, "\"abc\x00def\"", "illegal character NUL"},
	{"\"abc\x80def\"", token.STRING, 4, "\"abc\x80def\"", "illegal UTF-8 encoding"},
	{"\ufeff\ufeff", token.ILLEGAL, 3, "\ufeff\ufeff", "illegal byte order mark"}, // only first BOM is ignored
	{"//\ufeff", token.COMMENT, 2, "//\ufeff", "illegal byte order mark"},         // only first BOM is ignored
	// {"`a\ufeff`", IDENT, 2, "`a\ufeff`", "illegal byte order mark"},                                // only first BOM is ignored
	{`"` + "abc\ufeffdef" + `"`, token.STRING, 4, `"` + "abc\ufeffdef" + `"`, "illegal byte order mark"}, // only first BOM is ignored
}

func TestScanErrors(t *testing.T) {
	for _, e := range errorTests {
		t.Run(e.src, func(t *testing.T) {
			checkError(t, e.src, e.tok, e.pos, e.lit, e.err)
		})
	}
}

// Verify that no comments show up as literal values when skipping comments.
func TestNoLiteralComments(t *testing.T) {
	var src = `
		a: {
			A: 1 // foo
		}

		#b: {
			B: 2
			// foo
		}

		c: 3 // foo

		d: 4
		// foo

		b anycode(): {
		// foo
		}
	`
	var s Scanner
	s.Init(token.NewFile("", -1, len(src)), []byte(src), nil, 0)
	for {
		pos, tok, lit := s.Scan()
		class := tokenclass(tok)
		if lit != "" && class != keyword && class != literal && tok != token.COMMA {
			t.Errorf("%s: tok = %s, lit = %q", pos, tok, lit)
		}
		if tok <= token.EOF {
			break
		}
	}
}

func BenchmarkScan(b *testing.B) {
	b.StopTimer()
	file := token.NewFile("", -1, len(source))
	var s Scanner
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		s.Init(file, source, nil, ScanComments)
		for {
			_, tok, _ := s.Scan()
			if tok == token.EOF {
				break
			}
		}
	}
}

func BenchmarkScanFile(b *testing.B) {
	b.StopTimer()
	const filename = "go"
	src, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	file := token.NewFile(filename, -1, len(src))
	b.SetBytes(int64(len(src)))
	var s Scanner
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		s.Init(file, src, nil, ScanComments)
		for {
			_, tok, _ := s.Scan()
			if tok == token.EOF {
				break
			}
		}
	}
}
