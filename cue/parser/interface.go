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

// This file contains the exported entry points for invoking the

package parser

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// If src != nil, readSource converts src to a []byte if possible;
// otherwise it returns an error. If src == nil, readSource returns
// the result of reading the file specified by filename.
//
func readSource(filename string, src interface{}) ([]byte, error) {
	if src != nil {
		switch s := src.(type) {
		case string:
			return []byte(s), nil
		case []byte:
			return s, nil
		case *bytes.Buffer:
			// is io.Reader, but src is already available in []byte form
			if s != nil {
				return s.Bytes(), nil
			}
		case io.Reader:
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, s); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("invalid source type %T", src)
	}
	return ioutil.ReadFile(filename)
}

// Option specifies a parse option.
type Option func(p *parser)

var (
	// PackageClauseOnly causes parsing to stop after the package clause.
	PackageClauseOnly Option = packageClauseOnly
	packageClauseOnly        = func(p *parser) {
		p.mode |= packageClauseOnlyMode
	}

	// ImportsOnly causes parsing to stop parsing after the import declarations.
	ImportsOnly Option = importsOnly
	importsOnly        = func(p *parser) {
		p.mode |= importsOnlyMode
	}

	// ParseComments causes comments to be parsed.
	ParseComments Option = parseComments
	parseComments        = func(p *parser) {
		p.mode |= parseCommentsMode
	}

	// Trace causes parsing to print a trace of parsed productions.
	Trace    Option = traceOpt
	traceOpt        = func(p *parser) {
		p.mode |= traceMode
	}

	// DeclarationErrors causes parsing to report declaration errors.
	DeclarationErrors Option = declarationErrors
	declarationErrors        = func(p *parser) {
		p.mode |= declarationErrorsMode
	}

	// AllErrors causes all errors to be reported (not just the first 10 on different lines).
	AllErrors Option = allErrors
	allErrors        = func(p *parser) {
		p.mode |= allErrorsMode
	}

	// AllowPartial allows the parser to be used on a prefix buffer.
	AllowPartial Option = allowPartial
	allowPartial        = func(p *parser) {
		p.mode |= partialMode
	}
)

// A mode value is a set of flags (or 0).
// They control the amount of source code parsed and other optional
// parser functionality.
type mode uint

const (
	packageClauseOnlyMode mode = 1 << iota // stop parsing after package clause
	importsOnlyMode                        // stop parsing after import declarations
	parseCommentsMode                      // parse comments and add them to AST
	partialMode
	traceMode             // print a trace of parsed productions
	declarationErrorsMode // report declaration errors
	allErrorsMode         // report all errors (not just the first 10 on different lines)
)

// ParseFile parses the source code of a single CUE source file and returns
// the corresponding File node. The source code may be provided via
// the filename of the source file, or via the src parameter.
//
// If src != nil, ParseFile parses the source from src and the filename is
// only used when recording position information. The type of the argument
// for the src parameter must be string, []byte, or io.Reader.
// If src == nil, ParseFile parses the file specified by filename.
//
// The mode parameter controls the amount of source text parsed and other
// optional parser functionality. Position information is recorded in the
// file set fset, which must not be nil.
//
// If the source couldn't be read, the returned AST is nil and the error
// indicates the specific failure. If the source was read but syntax
// errors were found, the result is a partial AST (with Bad* nodes
// representing the fragments of erroneous source code). Multiple errors
// are returned via a ErrorList which is sorted by file position.
func ParseFile(p *token.FileSet, filename string, src interface{}, mode ...Option) (f *ast.File, err error) {
	if p == nil {
		panic("ParseFile: no file.FileSet provided (fset == nil)")
	}

	// get source
	text, err := readSource(filename, src)
	if err != nil {
		return nil, err
	}

	var pp parser
	defer func() {
		if pp.panicking {
			recover()
		}

		// set result values
		if f == nil {
			// source is not a valid Go source file - satisfy
			// ParseFile API and return a valid (but) empty
			// *File
			f = &ast.File{
				Name: new(ast.Ident),
				// Scope: NewScope(nil),
			}
		}

		pp.errors.Sort()
		err = pp.errors.Err()
	}()

	// parse source
	pp.init(p, filename, text, mode)
	f = pp.parseFile()
	if f == nil {
		return nil, pp.errors
	}
	f.Filename = filename
	resolve(f, pp.error)

	return
}

// ParseExpr is a convenience function for parsing an expression.
// The arguments have the same meaning as for Parse, but the source must
// be a valid CUE (type or value) expression. Specifically, fset must not
// be nil.
func ParseExpr(fset *token.FileSet, filename string, src interface{}, mode ...Option) (ast.Expr, error) {
	if fset == nil {
		panic("ParseExprFrom: no file.FileSet provided (fset == nil)")
	}

	// get source
	text, err := readSource(filename, src)
	if err != nil {
		return nil, err
	}

	var p parser
	defer func() {
		if p.panicking {
			recover()
		}
		p.errors.Sort()
		err = p.errors.Err()
	}()

	// parse expr
	p.init(fset, filename, text, mode)
	// Set up pkg-level scopes to avoid nil-pointer errors.
	// This is not needed for a correct expression x as the
	// parser will be ok with a nil topScope, but be cautious
	// in case of an erroneous x.
	e := p.parseRHS()

	// If a comma was inserted, consume it;
	// report an error if there's more tokens.
	if p.tok == token.COMMA && p.lit == "\n" {
		p.next()
	}
	if p.mode&partialMode == 0 {
		p.expect(token.EOF)
	}

	if p.errors.Len() > 0 {
		p.errors.Sort()
		return nil, p.errors.Err()
	}
	resolveExpr(e, p.error)

	return e, nil
}

// parseExprString is a convenience function for obtaining the AST of an
// expression x. The position information recorded in the AST is undefined. The
// filename used in error messages is the empty string.
func parseExprString(x string) (ast.Expr, error) {
	return ParseExpr(token.NewFileSet(), "", []byte(x))
}
