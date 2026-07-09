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

package cue

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/subsume"
)

// Value holds a CUE value. The zero Value is invalid.
//
// A Value is a lazy, immutable description of a computation over CUE
// values. Methods that return Values — Unify, FillPath, LookupPath,
// Eval, Default — construct new descriptions and never evaluate.
// Evaluation happens only at forcing methods: those whose answers leave
// the value domain (an error, a bool, a Go value, a syntax tree), all of
// which take a [context.Context] carrying cancellation and an optional
// stats recorder. Errors are values in CUE (bottom) and flow through the
// lazy algebra like any other value; they become Go errors only when
// forced. Laziness is unobservable except in cost: the algebra is pure,
// so forcing earlier or later cannot change any result.
//
// Values are cheap to copy. A Value belongs to the loader that created
// it; values from different loaders may not be combined.
type Value struct {
	rt *runtime.Runtime // owning runtime; nil for the zero Value and for values created by Errorf
	op *valueOp
}

// opKind enumerates the description nodes a Value can be built from.
type opKind uint8

const (
	opInvalid  opKind = iota
	opVertex          // a leaf: an existing vertex
	opUnify           // unify(a, b)
	opFillPath        // fill(a, path, x)
	opLookup          // lookup(a, path)
	opEval            // eval(a)
	opDefault         // default(a)
	opError           // an error value created by Errorf
)

// A valueOp is one node in the immutable description of a Value.
// Realization and forcing state is memoized on the node; Values may be
// shared across goroutines, so that state is guarded by a mutex.
type valueOp struct {
	kind   opKind
	a, b   *valueOp     // operands
	path   Path         // for opFillPath, opLookup
	fill   any          // for opFillPath: a Value, a Func, or a Go value
	opts   []Option     // for opEval
	err    errors.Error // for opError
	vertex *adt.Vertex  // for opVertex

	// mu guards the fields below it.
	mu sync.Mutex
	// memoRT is the runtime the memoized state below was computed
	// for. It only differs from the owning Value's runtime for
	// runtime-free values (Errorf) that are shared between runtimes,
	// in which case the memo is recomputed.
	memoRT *runtime.Runtime
	// built is the realized vertex: the structural result of the
	// operation, possibly not yet evaluated.
	built *adt.Vertex
	// needsEval reports whether built must be finalized when forced.
	needsEval bool
	// forced reports whether built has been finalized.
	forced bool
}

// newVertexValue returns a Value for an existing vertex, which may be
// unfinalized. It is the constructor used by loaders, via
// cuelang.org/go/internal/v2bridge.
func newVertexValue(rt *runtime.Runtime, w *adt.Vertex) Value {
	return Value{rt: rt, op: &valueOp{
		kind:   opVertex,
		vertex: w,
		memoRT: rt,
		built:  w,
		// A vertex leaf still needs a finalizing force; this is
		// cheap when the vertex is already finalized.
		needsEval: true,
	}}
}

// newForcedValue is like newVertexValue for a vertex that is known to
// be finalized already, such as an argument to a user function.
func newForcedValue(rt *runtime.Runtime, w *adt.Vertex) Value {
	v := newVertexValue(rt, w)
	v.op.forced = true
	return v
}

// Errorf creates an error value with the given message. It is useful for
// returning failures from user functions and package resolvers.
//
// The returned value has no runtime of its own: it adopts the runtime
// of any value it is combined with.
func Errorf(format string, args ...any) Value {
	return Value{op: &valueOp{
		kind: opError,
		err:  errors.Newf(token.NoPos, format, args...),
	}}
}

// String returns a description of the value without evaluating it — the
// value-level analogue of cueload's Source.String. Use Syntax to render
// an evaluated result.
func (v Value) String() string {
	if v.op == nil {
		return "<invalid>"
	}
	return v.op.describe(v.rt)
}

// Pos, Path, and Source are bookkeeping accessors: they answer from the
// description itself, without evaluating, and are best-effort on
// derived values.

// Pos reports the position of the value's dominant source, if known.
func (v Value) Pos() token.Pos {
	if v.op == nil {
		return token.NoPos
	}
	return v.op.pos()
}

// Path returns the path of v relative to its root, when the description
// determines one.
func (v Value) Path() Path {
	if v.op == nil {
		return Path{}
	}
	return Path{path: v.op.appendPath(nil, v.rt)}
}

// Source returns the syntax that gave rise to v, if there is exactly one.
func (v Value) Source() ast.Node {
	if v.op == nil {
		return nil
	}
	return v.op.source()
}

// Unify returns the greatest lower bound of v and w: the lazy value
// v & w. A conflict between v and w is an error value, observed when
// forced.
func (v Value) Unify(w Value) Value {
	if v.op == nil {
		return w
	}
	if w.op == nil {
		return v
	}
	rt := combinedRuntime(v, w)
	if v.op == w.op {
		// Unification is idempotent.
		return Value{rt: rt, op: v.op}
	}
	return Value{rt: rt, op: &valueOp{kind: opUnify, a: v.op, b: w.op}}
}

// FillPath returns a value with x unified at path p within v. x may be a
// Value, a [Func], or any Go value convertible to CUE.
func (v Value) FillPath(p Path, x any) Value {
	if v.op == nil {
		return v
	}
	rt := v.rt
	if xv, ok := x.(Value); ok {
		if xv.op == nil {
			return Value{rt: rt, op: &valueOp{
				kind: opError,
				err:  errors.Newf(token.NoPos, "cannot fill invalid (zero) cue.Value"),
			}}
		}
		rt = combinedRuntime(v, xv)
	}
	return Value{rt: rt, op: &valueOp{kind: opFillPath, a: v.op, path: p, fill: x}}
}

// LookupPath returns the value at path p within v. A missing or
// disallowed field yields an error value, observed when forced.
func (v Value) LookupPath(p Path) Value {
	if v.op == nil || len(p.path) == 0 {
		return v
	}
	return Value{rt: v.rt, op: &valueOp{kind: opLookup, a: v.op, path: p}}
}

// Eval returns the resolved form of v: references are resolved to their
// targets and, according to opts, disjunction defaults are selected.
// This is a semantic transformation — observable through ReferencePath
// and Syntax — and, like all Value-returning methods, lazy.
//
// No options are supported yet in this preview; passing any yields an
// error value.
func (v Value) Eval(opts ...Option) Value {
	if v.op == nil {
		return v
	}
	return Value{rt: v.rt, op: &valueOp{kind: opEval, a: v.op, opts: opts}}
}

// Default returns v with its default selected. If v has no default, the
// result is equivalent to v.
func (v Value) Default() Value {
	if v.op == nil {
		return v
	}
	return Value{rt: v.rt, op: &valueOp{kind: opDefault, a: v.op}}
}

// combinedRuntime returns the common runtime of two values, adopting
// the non-nil one when one value (such as an Errorf result) has no
// runtime. It panics when the values belong to different runtimes.
func combinedRuntime(v, w Value) *runtime.Runtime {
	switch {
	case v.rt == nil:
		return w.rt
	case w.rt == nil || v.rt == w.rt:
		return v.rt
	}
	panic("cue: cannot combine values created by different loaders")
}

// A forcer carries the parameters of one forcing operation down the
// description tree.
type forcer struct {
	rt    *runtime.Runtime
	goCtx context.Context
	// opCtx, if non-nil, is an existing evaluation context to reuse.
	// It is set when forcing happens within an in-flight call from
	// the evaluator into a user function (see Call).
	opCtx *adt.OpContext
}

// force realizes and evaluates v, returning the resulting vertex. The
// result is memoized. The returned error is non-nil only when the
// operation itself fails — currently only due to cancellation of ctx or
// forcing a zero or runtime-free composite value; CUE-level errors are
// represented in the vertex itself.
func (v Value) force(goCtx context.Context) (*adt.Vertex, error) {
	if v.op == nil {
		return nil, v.toErr(errNotExists)
	}
	if goCtx == nil {
		goCtx = context.Background()
	}
	f := &forcer{rt: v.rt, goCtx: goCtx}
	if c, ok := callFromContext(goCtx); ok && c.valid && c.rt == v.rt {
		f.opCtx = c.opCtx
	}
	return v.op.force(f)
}

func (op *valueOp) force(f *forcer) (*adt.Vertex, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.forceLocked(f)
}

func (op *valueOp) forceLocked(f *forcer) (*adt.Vertex, error) {
	if op.forced && op.memoRT == f.rt {
		return op.built, nil
	}
	if f.goCtx != nil {
		if err := f.goCtx.Err(); err != nil {
			// A canceled force is not memoized: a later force with a
			// live context may still succeed.
			return nil, canceledError(err)
		}
	}
	w, err := op.realizeLocked(f)
	if err != nil {
		return nil, err
	}
	if op.needsEval {
		if f.rt == nil {
			return nil, errors.Newf(token.NoPos,
				"cannot evaluate value that has no runtime")
		}
		opCtx := f.opCtx
		if opCtx == nil {
			opCtx = newOpContext(f.rt, f.goCtx, w)
			defer opCtx.FlushStats()
		}
		w.Finalize(opCtx)
		if b := opCtx.Canceled(); b != nil {
			// TODO: a canceled force currently poisons the underlying
			// vertex: its evaluation state is unspecified and is still
			// memoized as the realization of this node. A later change
			// may rebuild the vertex so that a subsequent force can
			// start afresh.
			err := f.goCtx.Err()
			if err == nil {
				err = context.Canceled
			}
			return nil, canceledError(err)
		}
	}
	op.forced = true
	return w, nil
}

// realize returns the vertex describing op. Operand values are forced
// in the process — their results are needed to wire up this node — but
// the resulting vertex itself is not evaluated when needsEval is left
// set; that happens on force. The result is memoized.
func (op *valueOp) realize(f *forcer) (*adt.Vertex, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.realizeLocked(f)
}

func (op *valueOp) realizeLocked(f *forcer) (*adt.Vertex, error) {
	if op.built != nil && op.memoRT == f.rt {
		return op.built, nil
	}
	// (Re-)realize, discarding any memoized state for another runtime.
	// This can only happen for runtime-free values shared between
	// runtimes.
	op.forced = false
	w, needsEval, err := op.construct(f)
	if err != nil {
		return nil, err
	}
	op.built, op.needsEval, op.memoRT = w, needsEval, f.rt
	return w, nil
}

// construct builds the vertex for op, wiring in the forced vertices of
// its operands. It reports whether the result still needs to be
// evaluated when forced. Structural problems (an invalid path, an
// unconvertible Go value) are returned as error vertices, not Go
// errors; the error return is reserved for failure of the forcing
// operation itself (see Value.force).
func (op *valueOp) construct(f *forcer) (_ *adt.Vertex, needsEval bool, _ error) {
	switch op.kind {
	case opVertex:
		return op.vertex, true, nil

	case opError:
		return errVertex(op.err), false, nil

	case opUnify:
		// The operands are forced first: the evaluator requires
		// vertices referenced as conjuncts of a new node to be
		// evaluated, just as in v1's Value.Unify.
		wa, err := op.a.force(f)
		if err != nil {
			return nil, false, err
		}
		wb, err := op.b.force(f)
		if err != nil {
			return nil, false, err
		}
		n := &adt.Vertex{}
		n.AddConjunct(adt.MakeRootConjunct(nil, wa))
		n.AddConjunct(adt.MakeRootConjunct(nil, wb))
		return n, true, nil

	case opLookup:
		// Force the operand, then walk its arcs, mirroring v1's
		// Value.LookupPath.
		wa, err := op.a.force(f)
		if err != nil {
			return nil, false, err
		}
		if err := op.path.Err(); err != nil {
			return errVertex(errors.Promote(err, "invalid path")), false, nil
		}
		opCtx := f.opCtx
		if opCtx == nil {
			opCtx = newOpContext(f.rt, f.goCtx, wa)
			defer opCtx.FlushStats()
		}
		n := wa
	outer:
		for _, sel := range op.path.path {
			ft, ferr := sel.feature(f.rt)
			if ferr != nil {
				return errVertex(ferr), false, nil
			}
			isConstraint := sel.kind == selAnyString || sel.kind == selAnyIndex
			deref := n.DerefValue()
			if len(deref.Arcs) == 0 {
				if b := deref.Bottom(); b != nil {
					// Errors propagate through lookup.
					return n, false, nil
				}
			}
			for _, arc := range deref.Arcs {
				if arc.Label == ft {
					if arc.IsConstraint() && !isConstraint {
						break
					}
					arc.Finalize(opCtx)
					n = arc
					continue outer
				}
			}
			if isConstraint {
				x := &adt.Vertex{
					Parent: n,
					Label:  ft,
				}
				deref.MatchAndInsert(opCtx, x)
				if x.HasConjuncts() {
					x.Finalize(opCtx)
					n = x
					continue
				}
			}
			b := mkErr(n, adt.EvalError, "field not found: %v", sel)
			if n.Accept(opCtx, ft) {
				b.Code = adt.IncompleteError
			}
			b.NotExists = true
			return errBottomVertex(b), false, nil
		}
		return n, false, nil

	case opFillPath:
		wa, err := op.a.force(f)
		if err != nil {
			return nil, false, err
		}
		if err := op.path.Err(); err != nil {
			return errVertex(errors.Promote(err, "invalid path")), false, nil
		}
		expr, errV, err := op.fillExpr(f)
		if err != nil {
			return nil, false, err
		}
		if errV != nil {
			return errV, false, nil
		}
		for i := len(op.path.path) - 1; i >= 0; i-- {
			sel := op.path.path[i]
			switch sel.kind {
			case selAnyString:
				expr = &adt.StructLit{Decls: []adt.Decl{
					&adt.BulkOptionalField{
						Filter: &adt.BasicType{K: adt.StringKind},
						Value:  expr,
					},
				}}
			case selAnyIndex:
				expr = &adt.ListLit{Elems: []adt.Elem{
					&adt.Ellipsis{Value: expr},
				}}
			case selIndex:
				list := &adt.ListLit{}
				any := &adt.Top{}
				for range sel.index {
					list.Elems = append(list.Elems, any)
				}
				list.Elems = append(list.Elems, expr, &adt.Ellipsis{})
				expr = list
			default:
				ft, err := sel.feature(f.rt)
				if err != nil {
					return errVertex(err), false, nil
				}
				expr = &adt.StructLit{Decls: []adt.Decl{
					&adt.Field{
						Label:   ft,
						Value:   expr,
						ArcType: adt.ArcMember,
					},
				}}
			}
		}
		n := &adt.Vertex{}
		n.AddConjunct(adt.MakeRootConjunct(nil, wa))
		n.AddConjunct(adt.MakeRootConjunct(nil, expr))
		return n, true, nil

	case opEval:
		w, err := op.a.force(f)
		if err != nil {
			return nil, false, err
		}
		if len(op.opts) > 0 {
			return errVertex(errors.Newf(token.NoPos,
				"Eval options are not implemented in this preview")), false, nil
		}
		return w.ToDataSingle(), false, nil

	case opDefault:
		w, err := op.a.force(f)
		if err != nil {
			return nil, false, err
		}
		return w.DerefValue().Default(), false, nil
	}
	return nil, false, errors.Newf(token.NoPos, "invalid cue.Value")
}

// fillExpr converts the x argument of FillPath to an expression. A
// conversion problem is reported as an error vertex.
func (op *valueOp) fillExpr(f *forcer) (adt.Expr, *adt.Vertex, error) {
	switch x := op.fill.(type) {
	case Value:
		w, err := x.op.realize(f)
		return w, nil, err
	case Func:
		v, err := x.adtValue(f.rt)
		if err != nil {
			return nil, errVertex(err), nil
		}
		return v, nil, nil
	case ast.Node:
		// Unlike v1, filling AST expressions (with scope resolution
		// relative to the fill path) is not supported.
		return nil, errVertex(errors.Newf(token.NoPos,
			"cannot use %T in FillPath: filling syntax is not supported; compile it to a Value first", x)), nil
	default:
		if f.rt == nil {
			return nil, nil, errors.Newf(token.NoPos,
				"cannot convert Go value in FillPath on a value that has no runtime")
		}
		opCtx := f.opCtx
		if opCtx == nil {
			opCtx = newOpContext(f.rt, f.goCtx, nil)
			defer opCtx.FlushStats()
		}
		return convert.FromGoValue(opCtx, x, true), nil, nil
	}
}

// errVertex returns a finalized vertex holding the given error.
func errVertex(err errors.Error) *adt.Vertex {
	return errBottomVertex(&adt.Bottom{Err: err})
}

// errBottomVertex returns a finalized vertex holding the given bottom.
func errBottomVertex(b *adt.Bottom) *adt.Vertex {
	node := &adt.Vertex{BaseValue: b}
	node.ForceDone()
	node.AddConjunct(adt.MakeRootConjunct(nil, b))
	return node
}

var printConfig = &debug.Config{Compact: true, CompactBuiltins: true}

func nodeFormat(r adt.Runtime, n adt.Node) string {
	return debug.NodeString(r, n, printConfig)
}

// newOpContext returns an evaluation context for one operation. If
// goCtx carries a stats recorder (see [stats.WithRecorder]), it
// overrides the runtime's recorder.
func newOpContext(rt *runtime.Runtime, goCtx context.Context, v *adt.Vertex) *adt.OpContext {
	opCtx := adt.New(v, &adt.Config{
		Runtime: rt,
		Format:  nodeFormat,
		Context: goCtx,
	})
	if goCtx != nil {
		if r, ok := stats.RecorderFromContext(goCtx); ok {
			opCtx.StatsRecorder = r
		}
	}
	return opCtx
}

// The remaining methods force evaluation: each evaluates as much of v as
// its answer requires.

// Exists reports whether v denotes a value at all. It reports false when
// evaluation fails or is cancelled; use Err to distinguish.
func (v Value) Exists(ctx context.Context) bool {
	w, err := v.force(ctx)
	if err != nil {
		return false
	}
	return w.Bottom() == nil
}

// Err forces v and returns its error if v is an error value, including
// cancellation of the evaluation itself.
func (v Value) Err(ctx context.Context) error {
	w, err := v.force(ctx)
	if err != nil {
		return err
	}
	if b := w.Bottom(); b != nil {
		return v.toErr(b)
	}
	return nil
}

// Kind returns the kind of v if it evaluates to a concrete value, and
// BottomKind otherwise. IncompleteKind returns the set of kinds that v
// may still take on.
func (v Value) Kind(ctx context.Context) Kind {
	w, err := v.force(ctx)
	if err != nil {
		return BottomKind
	}
	w = w.DerefValue()
	if w.BaseValue == nil || !w.IsConcrete() {
		return BottomKind
	}
	return kindFromADT(w.BaseValue.Kind())
}

func (v Value) IncompleteKind(ctx context.Context) Kind {
	w, err := v.force(ctx)
	if err != nil {
		return BottomKind
	}
	return kindFromADT(w.Kind())
}

// Validate reports whether v is valid according to opts (for instance,
// Concrete(true) requires the value to be fully specified). All errors
// are reported, subject to the configured limit. It is also the natural
// way to force a value once before concurrent use.
func (v Value) Validate(ctx context.Context, opts ...Option) error {
	var o options
	o.updateOptions(opts)
	if err := o.check("Validate", optConcrete|optFinal|optAll); err != nil {
		return err
	}
	w, err := v.force(ctx)
	if err != nil {
		return err
	}
	opCtx := newOpContext(v.rt, ctx, w)
	defer opCtx.FlushStats()
	b := adt.Validate(opCtx, w, &adt.ValidateConfig{
		Concrete:  o.concrete,
		Final:     o.final,
		AllErrors: true,
	})
	if b != nil {
		return v.toErr(b)
	}
	return nil
}

// Subsume reports whether w is an instance of v.
func (v Value) Subsume(ctx context.Context, w Value, opts ...Option) error {
	var o options
	o.updateOptions(opts)
	if err := o.check("Subsume", optFinal); err != nil {
		return err
	}
	vv, err := v.force(ctx)
	if err != nil {
		return err
	}
	ww, err := w.force(ctx)
	if err != nil {
		return err
	}
	p := subsume.CUE
	if o.final {
		p = subsume.Final
	}
	p.Defaults = true
	opCtx := newOpContext(v.rt, ctx, vv)
	defer opCtx.FlushStats()
	return p.Value(opCtx, vv, ww)
}

// Syntax converts the evaluated v to CUE syntax. It returns nil when
// forcing fails (for example due to cancellation); use Err to
// distinguish.
func (v Value) Syntax(ctx context.Context, opts ...Option) ast.Node {
	w, err := v.force(ctx)
	if err != nil {
		return nil
	}
	var o options
	o.updateOptions(opts)
	// All currently defined options apply to Syntax.

	p := export.Profile{
		Simplify:         true,
		TakeDefaults:     o.final,
		ShowOptional:     !o.omitOptional && !o.concrete,
		ShowDefinitions:  !o.omitDefinitions && !o.concrete,
		ShowHidden:       !o.omitHidden && !o.concrete,
		ShowAttributes:   !o.omitAttrs,
		ShowDocs:         o.docs,
		InlineImports:    o.inlineImports,
		ExpandReferences: o.concrete,
	}

	pkgID := ""
	if inst := v.rt.GetInstanceFromNode(w); inst != nil {
		pkgID = inst.ID()
	}

	var f *ast.File
	var exportErr error
	if o.concrete || o.final {
		f, exportErr = p.Vertex(v.rt, pkgID, w)
	} else {
		p.AddPackage = true
		f, exportErr = p.Def(v.rt, pkgID, w)
	}
	if exportErr != nil {
		x := &ast.BadExpr{}
		ast.AddComment(x, internalErrorComment(exportErr))
		return x
	}

	if len(f.Preamble()) > 0 {
		return f
	}
	if len(f.Decls) == 1 {
		if e, ok := f.Decls[0].(*ast.EmbedDecl); ok {
			for _, c := range ast.Comments(e) {
				ast.AddComment(f, c)
			}
			for _, c := range ast.Comments(e.Expr) {
				ast.AddComment(f, c)
			}
			ast.SetComments(e.Expr, ast.Comments(f))
			return e.Expr
		}
	}
	st := &ast.StructLit{Elts: f.Decls}
	ast.SetComments(st, ast.Comments(f))
	return st
}

func internalErrorComment(err error) *ast.CommentGroup {
	cg := &ast.CommentGroup{Doc: true}
	msg := fmt.Sprintf("internal error while exporting value: %v", err)
	for line := range strings.SplitSeq(msg, "\n") {
		cg.List = append(cg.List, &ast.Comment{Text: "// " + line})
	}
	return cg
}

// ReferencePath reports the value and path referred to by v if v is a
// reference, and the zero Value otherwise. Use the loader's PackageOf to
// identify the package of the returned root.
func (v Value) ReferencePath(ctx context.Context) (root Value, p Path) {
	w, err := v.force(ctx)
	if err != nil || w.IsData() {
		// A value in data mode, such as the result of [Value.Eval], has
		// had its references resolved, so it is no longer a reference to
		// another value regardless of whether structure sharing is
		// enabled.
		return Value{}, Path{}
	}
	c, count := w.SingleConjunct()
	if count != 1 {
		return Value{}, Path{}
	}
	opCtx := newOpContext(v.rt, ctx, w)
	defer opCtx.FlushStats()

	env, expr := c.EnvExpr()

	if sl, ok := expr.(*adt.StructLit); ok && sl.IsFile() && len(sl.Decls) == 1 {
		if e, ok := sl.Decls[0].(adt.Expr); ok {
			// The value is at the top level and it has a single
			// conjunct which is a StructLit representing the file
			// holding a single embedding that may be a reference.
			expr = e
		}
	}

	x, path := reference(v.rt, opCtx, env, expr)
	if x == nil {
		return Value{}, Path{}
	}
	return newForcedValue(v.rt, x), Path{path: path}
}

func reference(rt *runtime.Runtime, c *adt.OpContext, env *adt.Environment, r adt.Expr) (inst *adt.Vertex, path []Selector) {
	defer c.PopState(c.PushState(env, r.Source()))

	switch x := r.(type) {
	case *adt.NodeLink:
		inst, path = mkPath(rt, nil, x.Node)

	case *adt.FieldReference:
		env := c.Env(x.UpCount)
		inst, path = mkPath(rt, nil, env.Vertex)
		path = appendSelector(path, featureToSel(x.Label, rt))

	case *adt.LabelReference:
		env := c.Env(x.UpCount)
		return mkPath(rt, nil, env.Vertex)

	case *adt.DynamicReference:
		env := c.Env(x.UpCount)
		inst, path = mkPath(rt, nil, env.Vertex)
		v, _ := c.Evaluate(env, x.Label)
		path = appendSelector(path, valueToSel(v))

	case *adt.ImportReference:
		if x.Instance != nil {
			inst = rt.LoadInstance(x.Instance)
		} else {
			inst = rt.LoadBuiltin(rt.LabelStr(x.ImportPath))
		}

	case *adt.SelectorExpr:
		inst, path = reference(rt, c, env, x.X)
		path = appendSelector(path, featureToSel(x.Sel, rt))

	case *adt.IndexExpr:
		inst, path = reference(rt, c, env, x.X)
		v, _ := c.Evaluate(env, x.Index)
		path = appendSelector(path, valueToSel(v))
	}
	if inst == nil {
		return nil, nil
	}
	inst.Finalize(c)
	return inst, path
}

func mkPath(r *runtime.Runtime, a []Selector, v *adt.Vertex) (root *adt.Vertex, path []Selector) {
	if v.Parent == nil {
		return v, a
	}
	root, path = mkPath(r, a, v.Parent)
	path = appendSelector(path, featureToSel(v.Label, r))
	return root, path
}

func valueToSel(v adt.Value) Selector {
	switch x := adt.Unwrap(v).(type) {
	case *adt.Num:
		i, err := x.X.Int64()
		if err != nil {
			return errSel(errors.Promote(err, "invalid number"))
		}
		return Index(int(i))
	case *adt.String:
		return Str(x.Str)
	default:
		return errSel(errors.Newf(token.NoPos, "dynamic selector"))
	}
}

// Fields enumerates the fields of a struct value in canonical order.
// Enumeration forces the struct shallowly; the yielded values remain
// lazy. An enumeration failure yields no fields and is observable via
// Err.
//
// By default only regular, non-optional fields are enumerated; this can
// be changed with the [All], [Concrete], [Final], [Definitions],
// [Hidden], and [Optional] options. Passing any other option panics.
func (v Value) Fields(ctx context.Context, opts ...Option) iter.Seq2[Selector, Value] {
	o := options{
		omitDefinitions: true,
		omitHidden:      true,
		omitOptional:    true,
	}
	o.updateOptions(opts)
	if err := o.check("Fields", optAll|optConcrete|optFinal|optDefinitions|optHidden|optOptional); err != nil {
		panic(err)
	}
	return func(yield func(Selector, Value) bool) {
		w, err := v.force(ctx)
		if err != nil {
			return
		}
		opCtx := newOpContext(v.rt, ctx, w)
		defer opCtx.FlushStats()
		arcs, ferr := structArcs(opCtx, w, o)
		if ferr != nil {
			return
		}
		for _, arc := range arcs {
			arc.Finalize(opCtx)
			if !yield(featureToSel(arc.Label, v.rt), newForcedValue(v.rt, arc)) {
				return
			}
		}
	}
}

// structArcs mirrors the field selection logic of v1's structValOpts:
// it returns the arcs of obj to be enumerated according to o, in
// canonical order.
func structArcs(opCtx *adt.OpContext, obj *adt.Vertex, o options) ([]*adt.Vertex, *adt.Bottom) {
	obj = obj.DerefValue().Default()

	switch b := obj.Bottom(); {
	case b != nil && b.IsIncomplete() && !o.concrete && !o.final:
	// Allow scalar values if hidden or definition fields are requested.
	case !o.omitHidden, !o.omitDefinitions:
	default:
		if b := checkKindBottom(opCtx, obj, adt.StructKind); b != nil && !b.ChildError {
			return nil, b
		}
	}

	// Features are topologically sorted.
	features := export.VertexFeatures(opCtx, obj)
	arcs := make([]*adt.Vertex, 0, len(obj.Arcs))
	for _, f := range features {
		if f.IsLet() {
			continue
		}
		if f.IsDef() && (o.omitDefinitions || o.concrete) {
			continue
		}
		if f.IsHidden() && o.omitHidden {
			continue
		}
		arc := obj.LookupRaw(f)
		if arc == nil {
			continue
		}
		switch arc.ArcType {
		case adt.ArcOptional:
			if o.omitOptional {
				continue
			}
		case adt.ArcRequired:
			// We report an error for required fields if the
			// configuration is final or concrete. We also do so if
			// omitOptional is true, as it avoids hiding errors in
			// required fields.
			if o.omitOptional || o.concrete || o.final {
				arc = &adt.Vertex{
					Label:     f,
					Parent:    arc.Parent,
					Conjuncts: arc.Conjuncts,
					BaseValue: adt.NewRequiredNotPresentError(opCtx, arc),
				}
				arc.ForceDone()
			}
		}
		arcs = append(arcs, arc)
	}
	return arcs, nil
}

// Items enumerates the elements of a list value. A non-list value
// yields no elements.
func (v Value) Items(ctx context.Context) iter.Seq2[int, Value] {
	return func(yield func(int, Value) bool) {
		elems, err := v.listElems(ctx)
		if err != nil {
			return
		}
		for i, e := range elems {
			if !yield(i, e) {
				return
			}
		}
	}
}

// listElems returns the elements of a list value.
func (v Value) listElems(ctx context.Context) ([]Value, error) {
	w, err := v.forceDefault(ctx)
	if err != nil {
		return nil, err
	}
	opCtx := newOpContext(v.rt, ctx, w)
	defer opCtx.FlushStats()
	if b := checkKindBottom(opCtx, w, adt.ListKind); b != nil {
		return nil, v.toErr(b)
	}
	var elems []Value
	for e := range w.Elems() {
		e.Finalize(opCtx)
		elems = append(elems, newForcedValue(v.rt, e))
	}
	return elems, nil
}

// Concrete scalar accessors: forcing conversions out of the value
// domain. Note: the v1 String method becomes AsString, freeing String
// for the lazy description above.

// Bool returns the bool value of v or false and an error if v is not a
// boolean.
func (v Value) Bool(ctx context.Context) (bool, error) {
	w, err := v.forceDefault(ctx)
	if err != nil {
		return false, err
	}
	if b, _ := w.BaseValue.(*adt.Bool); b != nil {
		return b.B, nil
	}
	return false, v.checkKindErr(w, adt.BoolKind)
}

// AsString returns the string value if v is a string or an error
// otherwise. To render an arbitrary CUE value as text, use Syntax with
// [cuelang.org/go/cue/format].
func (v Value) AsString(ctx context.Context) (string, error) {
	w, err := v.forceDefault(ctx)
	if err != nil {
		return "", err
	}
	if s, _ := w.BaseValue.(*adt.String); s != nil {
		return s.Str, nil
	}
	return "", v.checkKindErr(w, adt.StringKind)
}

// Bytes returns a byte slice if v represents a list of bytes or a
// string, or an error otherwise.
func (v Value) Bytes(ctx context.Context) ([]byte, error) {
	w, err := v.forceDefault(ctx)
	if err != nil {
		return nil, err
	}
	switch x := w.BaseValue.(type) {
	case *adt.Bytes:
		return append([]byte(nil), x.B...), nil
	case *adt.String:
		return []byte(x.Str), nil
	}
	return nil, v.checkKindErr(w, adt.BytesKind|adt.StringKind)
}

// forceDefault forces v and returns the result with its default
// selected, mirroring the implicit defaulting of the v1 scalar
// accessors.
func (v Value) forceDefault(ctx context.Context) (*adt.Vertex, error) {
	w, err := v.force(ctx)
	if err != nil {
		return nil, err
	}
	return w.DerefValue().Default(), nil
}

// checkKindErr returns an error explaining why the forced vertex w is
// not a concrete value of one of the given kinds. It mirrors v1's
// Value.checkKind.
func (v Value) checkKindErr(w *adt.Vertex, want adt.Kind) error {
	if b := checkKindBottomRT(v.rt, w, want); b != nil {
		return v.toErr(b)
	}
	return nil
}

func checkKindBottomRT(rt *runtime.Runtime, w *adt.Vertex, want adt.Kind) *adt.Bottom {
	if rt == nil {
		if b := w.Bottom(); b != nil {
			return b
		}
		return errNotExists
	}
	opCtx := newOpContext(rt, nil, w)
	defer opCtx.FlushStats()
	return checkKindBottom(opCtx, w, want)
}

func checkKindBottom(opCtx *adt.OpContext, w *adt.Vertex, want adt.Kind) *adt.Bottom {
	x := w.Value()
	if b, ok := x.(*adt.Bottom); ok {
		return b
	}
	k := x.Kind()
	if want != adt.BottomKind {
		if k&want == adt.BottomKind {
			return mkErr(x, "cannot use value %v (type %s) as %s",
				opCtx.Str(x), k, want)
		}
		if !adt.IsConcrete(x) {
			return mkErr(x, adt.IncompleteError, "non-concrete value %v", k)
		}
	}
	return nil
}

// Description rendering, answered without forcing.

func (op *valueOp) describe(rt *runtime.Runtime) string {
	switch op.kind {
	case opVertex:
		if sels := op.appendPath(nil, rt); len(sels) > 0 {
			return Path{path: sels}.String()
		}
		return "value"
	case opError:
		msg := ""
		if op.err != nil {
			msg = op.err.Error()
		}
		return fmt.Sprintf("error(%q)", msg)
	case opUnify:
		return fmt.Sprintf("unify(%s, %s)", op.a.describe(rt), op.b.describe(rt))
	case opLookup:
		return fmt.Sprintf("lookup(%s, %s)", op.a.describe(rt), op.path)
	case opFillPath:
		var x string
		switch fill := op.fill.(type) {
		case Value:
			x = fill.String()
		case Func:
			name := fill.name
			if name == "" {
				name = "_"
			}
			x = "func " + name
		default:
			x = fmt.Sprintf("%v", fill)
		}
		return fmt.Sprintf("fill(%s, %s, %s)", op.a.describe(rt), op.path, x)
	case opEval:
		return fmt.Sprintf("eval(%s)", op.a.describe(rt))
	case opDefault:
		return fmt.Sprintf("default(%s)", op.a.describe(rt))
	}
	return "<invalid>"
}

func (op *valueOp) source() ast.Node {
	switch op.kind {
	case opVertex:
		c, count := op.vertex.SingleConjunct()
		var src ast.Node
		if count == 1 {
			src = c.Source()
		}
		if src == nil && op.vertex.BaseValue != nil {
			src = op.vertex.Value().Source()
		}
		return src
	case opError:
		return nil
	default:
		return op.a.source()
	}
}

func (op *valueOp) pos() token.Pos {
	switch op.kind {
	case opVertex:
		if src := op.source(); src != nil {
			if pos := src.Pos(); pos.IsValid() {
				return pos
			}
		}
		// Pick the most-concrete field.
		var p token.Pos
		for c := range op.vertex.LeafConjuncts() {
			pp := adt.Pos(c.Elem())
			if pp.IsValid() {
				p = pp
			}
		}
		return p
	case opError:
		if op.err != nil {
			return op.err.Position()
		}
		return token.NoPos
	default:
		return op.a.pos()
	}
}

// appendPath appends the path determined by the description, when there
// is one. It is best-effort on derived values: unification and filling
// report the path of their first operand, and lookup appends its
// selectors to the path of its operand.
func (op *valueOp) appendPath(a []Selector, rt *runtime.Runtime) []Selector {
	switch op.kind {
	case opVertex:
		return appendVertexPath(a, op.vertex, rt)
	case opError:
		return a
	case opLookup:
		a = op.a.appendPath(a, rt)
		for _, sel := range op.path.path {
			a = appendSelector(a, sel)
		}
		return a
	default:
		return op.a.appendPath(a, rt)
	}
}

func appendVertexPath(a []Selector, w *adt.Vertex, rt *runtime.Runtime) []Selector {
	if w.Parent != nil {
		a = appendVertexPath(a, w.Parent, rt)
	}
	if w.Label == 0 || rt == nil {
		// A Label may be 0 for programmatically inserted nodes.
		return a
	}
	if w.Label.IsLet() {
		return appendSelector(a, errSel(errors.Newf(token.NoPos,
			"let binding %q is not addressable", w.Label.IdentString(rt))))
	}
	return appendSelector(a, featureToSel(w.Label, rt))
}
