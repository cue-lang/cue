// Copyright 2020 CUE Authors
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

package adt

// This file contains error encodings.
//
//
// *Bottom:
//    - an adt.Value
//    - always belongs to a single vertex.
//    - does NOT implement error
//    - marks error code used for control flow
//
// errors.Error
//    - CUE default error
//    - implements error
//    - tracks error locations
//    - has error message details
//    - supports multiple errors
//

import (
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	cueformat "cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/iterutil"
)

// ErrorCode indicates the type of error. The type of error may influence
// control flow. No other aspects of an error may influence control flow.
type ErrorCode int8

//go:generate go tool stringer -type=ErrorCode -linecomment

const (
	// An EvalError is a fatal evaluation error.
	EvalError ErrorCode = iota // eval

	// A UserError is a fatal error originating from the user using the error
	// builtin.
	UserError // user

	// A LegacyUserError is a fatal error originating from the user using the
	// _|_ token, which we intend to phase out.
	LegacyUserError // user

	// StructuralCycleError means a structural cycle was found. Structural
	// cycles are permanent errors, but they are not passed up recursively,
	// as a unification of a value with a structural cycle with one that
	// doesn't may still give a useful result.
	StructuralCycleError // structural cycle

	// IncompleteError means an evaluation could not complete because of
	// insufficient information that may still be added later.
	IncompleteError // incomplete

	// A CycleError indicates a reference error. It is considered to be
	// an incomplete error, as reference errors may be broken by providing
	// a concrete value.
	CycleError // cycle
)

// Bottom represents an error or bottom symbol.
//
// Although a Bottom node holds control data, it should not be created until the
// control information already resulted in an error.
type Bottom struct {
	Src ast.Node
	Err errors.Error

	Code ErrorCode
	// Permanent indicates whether an incomplete error can be
	// resolved later without making the configuration more specific.
	// This may happen when an arc isn't fully resolved yet.
	Permanent    bool
	HasRecursive bool
	ChildError   bool // Err is the error of the child
	NotExists    bool // This error originated from a failed lookup.
	CloseCheck   bool // This error resulted from a close check.
	ForCycle     bool // this is a for cycle
	// Value holds the computed value so far in case
	Value Value

	// Node marks the node at which an error occurred. This is used to
	// determine the package to which an error belongs.
	// TODO: use a more precise mechanism for tracking the package.
	Node *Vertex
}

func (x *Bottom) Source() ast.Node { return x.Src }
func (x *Bottom) Kind() Kind       { return BottomKind }

func (b *Bottom) IsIncomplete() bool {
	if b == nil {
		return false
	}
	return b.Code == IncompleteError || b.Code == CycleError
}

// isLiteralBottom reports whether x is an error originating from a user.
func isLiteralBottom(x Expr) bool {
	b, ok := x.(*Bottom)
	return ok && b.Code == LegacyUserError
}

// isError reports whether v is an error or nil.
func isError(v Value) bool {
	if v == nil {
		return true
	}
	_, ok := v.(*Bottom)
	return ok
}

// isIncomplete reports whether v is associated with an incomplete error.
func isIncomplete(v *Vertex) bool {
	if v == nil {
		return true
	}
	if b := v.Bottom(); b != nil {
		return b.IsIncomplete()
	}
	return false
}

// AddChildError updates x to record an error that occurred in one of
// its descendent arcs. The resulting error will record the worst error code of
// the current error or recursive error.
//
// If x is not already an error, the value is recorded in the error for
// reference.
func (n *nodeContext) AddChildError(recursive *Bottom) {
	v := n.node
	v.ChildErrors = CombineErrors(nil, v.ChildErrors, recursive)
	if recursive.IsIncomplete() {
		return
	}
	x := v.BaseValue
	err, _ := x.(*Bottom)
	if err == nil || err.CloseCheck {
		n.setBaseValue(&Bottom{
			Code:         recursive.Code,
			Value:        v,
			HasRecursive: true,
			ChildError:   true,
			CloseCheck:   recursive.CloseCheck,
			Err:          recursive.Err,
			Node:         n.node,
		})
		return
	}

	err.HasRecursive = true
	if err.Code > recursive.Code {
		err.Code = recursive.Code
	}

	n.setBaseValue(err)
}

// CombineErrors combines two errors that originate at the same Vertex.
func CombineErrors(src ast.Node, x, y Value) *Bottom {
	a, _ := Unwrap(x).(*Bottom)
	b, _ := Unwrap(y).(*Bottom)

	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	case a == b && isCyclePlaceholder(a):
		return a
	case a == b:
		// Don't return a (or b) because they may have other non-nil fields.
		return &Bottom{
			Src:  src,
			Err:  a.Err,
			Code: a.Code,
		}
	}

	if a.Code != b.Code {
		if a.Code > b.Code {
			a, b = b, a
		}

		if b.Code >= IncompleteError {
			return a
		}
	}

	return &Bottom{
		Src:        src,
		Err:        errors.Append(a.Err, b.Err),
		Code:       a.Code,
		CloseCheck: a.CloseCheck || b.CloseCheck,
	}
}

func addPositions(ctx *OpContext, err *ValueError, c Conjunct) {
	switch x := c.x.(type) {
	case *Field:
		// if x.ArcType == ArcRequired {
		err.AddPosition(c.x)
		// }
	case *ConjunctGroup:
		for _, c := range *x {
			addPositions(ctx, err, c)
		}
	}
	if p := c.CloseInfo.Location(ctx); p != nil {
		err.AddPosition(p)
	}
}

func NewRequiredNotPresentError(ctx *OpContext, v *Vertex, morePositions ...Node) *Bottom {
	saved := ctx.PushArc(v)
	err := ctx.Newf("field is required but not present")
	for _, p := range morePositions {
		err.AddPosition(p)
	}
	for c := range v.LeafConjuncts() {
		if f, ok := c.x.(*Field); ok && f.ArcType == ArcRequired {
			err.AddPosition(c.x)
		}
		if p := c.CloseInfo.Location(ctx); p != nil {
			err.AddPosition(p)
		}
	}

	b := &Bottom{
		Code: IncompleteError,
		Err:  err,
		Node: v,
	}
	ctx.PopArc(saved)
	return b
}

func newRequiredFieldInComprehensionError(ctx *OpContext, x *ForClause, v *Vertex) *Bottom {
	err := ctx.Newf("missing required field in for comprehension: %v", v.Label)
	err.AddPosition(x.Src)
	for c := range v.LeafConjuncts() {
		addPositions(ctx, err, c)
	}
	return &Bottom{
		Code: IncompleteError,
		Err:  err,
	}
}

func (v *Vertex) reportFieldIndexError(c *OpContext, pos token.Pos, f Feature) {
	v.reportFieldError(c, pos, f,
		"index out of range [%d] with length %d",
		"undefined field: %s")
}

func (v *Vertex) reportFieldCycleError(c *OpContext, pos token.Pos, f Feature) *Bottom {
	const msg = "cyclic reference to field %[1]v"
	b := v.reportFieldError(c, pos, f, msg, msg)
	return b
}

func (v *Vertex) reportFieldError(c *OpContext, pos token.Pos, f Feature, intMsg, stringMsg string) *Bottom {
	code := IncompleteError
	// If v is an error, we need to adopt the worst error.
	if b := v.Bottom(); b != nil && !isCyclePlaceholder(b) {
		code = b.Code
	} else if !v.Accept(c, f) {
		code = EvalError
	}

	label := f.SelectorString(c.Runtime)

	var err errors.Error
	if f.IsInt() {
		err = c.NewPosf(pos, intMsg, f.Index(), iterutil.Count(v.Elems()))
	} else {
		err = c.NewPosf(pos, stringMsg, label)
	}
	b := &Bottom{
		Code: code,
		Err:  err,
		Node: v,
	}
	// TODO: yield failure
	c.AddBottom(b) // TODO: unify error mechanism.
	return b
}

// A ValueError is returned as a result of evaluating a value.
type ValueError struct {
	r       Runtime
	v       *Vertex
	pos     token.Pos
	auxpos  []token.Pos
	altPath []string
	errors.Message
}

func (v *ValueError) AddPosition(n Node) {
	if n == nil {
		return
	}
	v.AddPos(pos(n))
}

func (v *ValueError) AddPos(p token.Pos) {
	if p != token.NoPos {
		if slices.Contains(v.auxpos, p) {
			return
		}
		v.auxpos = append(v.auxpos, p)
	}
}

func (v *ValueError) AddClosedPositions(ctx *OpContext, c CloseInfo) {
	for n := range c.AncestorPositions(ctx) {
		v.AddPosition(n)
	}
}

func (c *OpContext) errNode() *Vertex {
	return c.vertex
}

// MarkPositions marks the current position stack.
func (c *OpContext) MarkPositions() int {
	return len(c.positions)
}

// ReleasePositions sets the position state to one from a call to MarkPositions.
func (c *OpContext) ReleasePositions(p int) {
	c.positions = c.positions[:p]
}

func (c *OpContext) AddPosition(n Node) {
	if n != nil {
		c.positions = append(c.positions, n)
	}
}

func (c *OpContext) Newf(format string, args ...interface{}) *ValueError {
	return c.NewPosf(c.pos(), format, args...)
}

func appendNodePositions(a []token.Pos, n Node) []token.Pos {
	if p := pos(n); p != token.NoPos {
		a = append(a, p)
	}
	if v, ok := n.(*Vertex); ok {
		for c := range v.LeafConjuncts() {
			a = appendNodePositions(a, c.Elem())
		}
	}
	return a
}

func (c *OpContext) NewPosf(p token.Pos, format string, args ...interface{}) *ValueError {
	var a []token.Pos
	if len(c.positions) > 0 {
		a = make([]token.Pos, 0, len(c.positions))
		for _, n := range c.positions {
			a = appendNodePositions(a, n)
		}
	}
	for i, arg := range args {
		switch x := arg.(type) {
		case Node:
			a = appendNodePositions(a, x)
			// Wrap nodes in a [fmt.Stringer] which delays the call to
			// [OpContext.Str] until the error needs to be rendered.
			// This helps avoid work, as in many cases,
			// errors are created but never shown to the user.
			//
			// A Vertex will set an error as its BaseValue via a Bottom node,
			// which might be this error we are creating.
			// Using the Vertex directly could then lead to endless recursion.
			// Make a shallow copy to avoid that.
			if v, ok := x.(*Vertex); ok {
				// TODO(perf): we could join this allocation with the creation
				// of the stringer below.
				vcopy := *v
				x = &vcopy
			}
			args[i] = c.Str(x)
		case ast.Node:
			// TODO: ideally the core evaluator should not depend on higher
			// level packages. This will allow the debug packages to be used
			// more widely.
			b, _ := cueformat.Node(x)
			if p := x.Pos(); p != token.NoPos {
				a = append(a, p)
			}
			args[i] = string(b)
		case Feature:
			args[i] = x.SelectorString(c.Runtime)
		}
	}

	return &ValueError{
		r:       c.Runtime,
		v:       c.errNode(),
		pos:     p,
		auxpos:  a,
		altPath: c.makeAltPath(),
		Message: errors.NewMessagef(format, args...),
	}
}

func (c *OpContext) makeAltPath() (a []string) {
	if len(c.altPath) == 0 {
		return nil
	}

	for _, f := range appendPath(nil, c.altPath[0]) {
		a = append(a, f.SelectorString(c))
	}
	for _, v := range c.altPath[1:] {
		if f := v.Label; f != 0 {
			a = append(a, f.SelectorString(c))
		}
	}
	return a
}

func (e *ValueError) Error() string {
	return errors.String(e)
}

func (e *ValueError) Position() token.Pos {
	return e.pos
}

func (e *ValueError) InputPositions() (a []token.Pos) {
	return e.auxpos
}

func (e *ValueError) Path() (a []string) {
	if len(e.altPath) > 0 {
		return e.altPath
	}
	if e.v == nil {
		return nil
	}
	for _, f := range appendPath(nil, e.v) {
		a = append(a, f.SelectorString(e.r))
	}
	return a
}
