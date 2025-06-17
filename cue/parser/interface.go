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
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/internal/source"
)

// Option specifies a parse option.
type Option interface {
	apply(cfg *Config)
}

var _ Option = Config{}

// Config represents the end result of applying a set of options.
// The zero value is not OK to use: use [NewConfig] to construct
// a Config value before using it.
//
// Config itself implements [Option] by overwriting the
// entire configuration.
//
// Config is comparable.
type Config struct {
	// valid is set by NewConfig and is used to check
	// that a Config has been created correctly.
	valid bool

	// Mode holds a bitmask of boolean parser options.
	Mode Mode

	// Version holds the language version to use when
	// parsing the CUE syntax.
	Version string
}

// Ensure that Config is comparable.
var _ = Config{} == Config{}

// apply implements [Option]
func (cfg Config) apply(cfg1 *Config) {
	if !cfg.valid {
		panic("zero parser.Config value used; use parser.NewConfig!")
	}
	*cfg1 = cfg
}

// NewConfig returns the configuration containing all default values
// with the given options applied.
func NewConfig(opts ...Option) Config {
	return Config{
		valid:   true,
		Version: cueversion.LanguageVersion(),
	}.Apply(opts...)
}

// Apply applies all the given options to cfg and
// returns the resulting configuration.
func (cfg Config) Apply(opts ...Option) Config {
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	return cfg
}

// IsValid reports whether cfg is valid; that
// is, it has been created with [NewConfig].
func (cfg Config) IsValid() bool {
	return cfg.valid
}

// optionFunc implements [Option] for a function.
type optionFunc func(cfg *Config)

func (f optionFunc) apply(cfg *Config) {
	f(cfg)
}

// A Mode value is a set of flags (or 0).
// It controls the amount of source code parsed and other optional
// parser functionality.
//
// Mode implements [Option] by or-ing all its bits
// with [Config.Mode].
type Mode uint

const (
	// PackageClauseOnly causes parsing to stop after the package clause.
	PackageClauseOnly Mode = 1 << iota

	// ImportsOnly causes parsing to stop parsing after the import declarations.
	ImportsOnly

	// ParseComments causes comments to be parsed.
	ParseComments

	// ParseFuncs causes function declarations to be parsed.
	//
	// This is an experimental function and the API is likely to
	// change or dissapear.
	ParseFuncs

	// Trace causes parsing to print a trace of parsed productions.
	Trace

	// DeclarationErrors causes parsing to report declaration errors.
	DeclarationErrors

	// AllErrors causes all errors to be reported (not just the first 10 on different lines).
	AllErrors

	// AllowPartial allows the parser to be used on a prefix buffer.
	AllowPartial
)

// apply implements [Option].
func (m Mode) apply(c *Config) {
	c.Mode |= m
}

// Version specifies the language version to use when parsing
// the CUE. The argument must be a valid semantic version, as
// checked by [semver.IsValid].
//
// The version will be recorded in the [ast.File] returned
// from [ParseFile].
func Version(v string) Option {
	if !semver.IsValid(v) {
		panic(fmt.Errorf("invalid language version %q", v))
	}
	return optionFunc(func(c *Config) {
		c.Version = v
	})
}

// FromVersion specifies until which legacy version the parser should provide
// backwards compatibility.
// Deprecated: use [Version] instead.
func FromVersion(version int) Option {
	return optionFunc(func(cfg *Config) {})
}

// DeprecationError is a sentinel error to indicate that an error is
// related to an unsupported old CUE syntax.
type DeprecationError struct {
	Version int
}

func (e *DeprecationError) Error() string {
	return "try running `cue fix` (possibly with an earlier version, like v0.2.2) to upgrade"
}

const (
	// Deprecated: see [Version].
	Latest = 0

	// Deprecated: see [Version].
	FullBackwardCompatibility = 0
)

// FileOffset specifies the File position info to use.
//
// Deprecated: this has no effect.
func FileOffset(pos int) Option {
	return optionFunc(func(*Config) {})
}

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
func ParseFile(filename string, src interface{}, mode ...Option) (f *ast.File, err error) {

	// get source
	text, err := source.ReadAll(filename, src)
	if err != nil {
		return nil, err
	}

	var pp parser
	defer func() {
		if pp.panicking {
			_ = recover()
		}

		// set result values
		if f == nil {
			// source is not a valid CUE source file - satisfy
			// ParseFile API and return a valid (but) empty *File
			f = &ast.File{
				// Scope: NewScope(nil),
			}
		}

		err = errors.Sanitize(pp.errors)
	}()

	// parse source
	pp.init(filename, text, mode)
	f = pp.parseFile()
	if f == nil {
		return nil, pp.errors
	}
	f.Filename = filename
	astutil.Resolve(f, pp.errf)

	return f, pp.errors
}

// ParseExpr is a convenience function for parsing an expression.
// The arguments have the same meaning as for Parse, but the source must
// be a valid CUE (type or value) expression. Specifically, fset must not
// be nil.
func ParseExpr(filename string, src interface{}, mode ...Option) (ast.Expr, error) {
	// get source
	text, err := source.ReadAll(filename, src)
	if err != nil {
		return nil, err
	}

	var p parser
	defer func() {
		if p.panicking {
			_ = recover()
		}
		err = errors.Sanitize(p.errors)
	}()

	// parse expr
	p.init(filename, text, mode)
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
	if p.cfg.Mode&AllowPartial == 0 {
		p.expect(token.EOF)
	}

	if p.errors != nil {
		return nil, p.errors
	}
	astutil.ResolveExpr(e, p.errf)

	return e, p.errors
}
