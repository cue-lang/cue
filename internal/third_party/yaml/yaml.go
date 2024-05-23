// Package yaml implements YAML support for the Go language.
//
// Source code and other details for the project are available at GitHub:
//
//	https://github.com/go-yaml/yaml
package yaml

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
)

// Unmarshal decodes the first document found within the in byte slice
// and returns it as a CUE syntax AST.
func Unmarshal(filename string, in []byte) (expr ast.Expr, err error) {
	return unmarshal(filename, in)
}

// A Decoder reads and decodes YAML values from an input stream.
type Decoder struct {
	strict    bool
	firstDone bool
	parser    *parser
}

// NewDecoder returns a new decoder that reads from r.
//
// The decoder introduces its own buffering and may read
// data from r beyond the YAML values requested.
func NewDecoder(filename string, src interface{}) (*Decoder, error) {
	d, err := newParser(filename, src)
	if err != nil {
		return nil, err
	}
	return &Decoder{parser: d}, nil
}

// Decode reads the next YAML-encoded value from its input and returns
// it as CUE syntax. It returns io.EOF if there are no more value in the
// stream.
func (dec *Decoder) Decode() (expr ast.Expr, err error) {
	d := newDecoder(dec.parser)
	defer handleErr(&err)
	node := dec.parser.parse()
	if node == nil {
		if !dec.firstDone {
			expr = ast.NewNull()
		}
		return expr, io.EOF
	}
	dec.firstDone = true
	expr = d.unmarshal(node)
	if len(d.terrors) > 0 {
		return nil, &TypeError{d.terrors}
	}
	return expr, nil
}

func unmarshal(filename string, in []byte) (expr ast.Expr, err error) {
	defer handleErr(&err)
	p, err := newParser(filename, in)
	if err != nil {
		return nil, err
	}
	defer p.destroy()
	node := p.parse()
	d := newDecoder(p)
	if node != nil {
		expr = d.unmarshal(node)
	}
	if len(d.terrors) > 0 {
		return nil, &TypeError{d.terrors}
	}
	return expr, nil
}

func handleErr(err *error) {
	if v := recover(); v != nil {
		if e, ok := v.(yamlError); ok {
			*err = e.err
		} else {
			panic(v)
		}
	}
}

type yamlError struct {
	err error
}

func (p *parser) failf(line int, format string, args ...interface{}) {
	where := p.parser.filename + ":"
	line++
	where += strconv.Itoa(line) + ": "
	panic(yamlError{fmt.Errorf(where+format, args...)})
}

// A TypeError is returned by Unmarshal when one or more fields in
// the YAML document cannot be properly decoded into the requested
// types. When this error is returned, the value is still
// unmarshaled partially.
type TypeError struct {
	Errors []string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("yaml: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}
