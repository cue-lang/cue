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

package cueload

// This file implements the interpreter for the Source algebra: see
// Loader.Load. Sources are evaluated as streams of loadItems. Two
// kinds of error flow through a stream:
//
//   - per-item errors (loadItem.err): an evaluation problem with one
//     value, such as a failing validation or a broken package; the
//     stream continues.
//   - structural errors (the error side of the internal iterators): a
//     problem with the load plan itself, such as an unmatched pattern
//     or an unknown codec; the stream ends.

import (
	"context"
	"fmt"
	"iter"

	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// A loadItem is one element of an interpreted Source stream.
type loadItem struct {
	v         cue.Value
	origin    Origin
	hasOrigin bool
	err       error // per-item error
}

// run interprets op, wrapping runOp with cancellation checks. A
// non-nil error yielded on the second position is structural and ends
// the stream.
func (l *Loader) run(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(loadItem{}, err)
			return
		}
		for item, fatal := range l.runOp(ctx, op) {
			if err := ctx.Err(); err != nil {
				yield(loadItem{}, err)
				return
			}
			if !yield(item, fatal) || fatal != nil {
				return
			}
		}
	}
}

// materialize collects the full stream denoted by op.
func (l *Loader) materialize(ctx context.Context, op *sourceOp) ([]loadItem, error) {
	var items []loadItem
	for item, fatal := range l.run(ctx, op) {
		if fatal != nil {
			return nil, fatal
		}
		items = append(items, item)
	}
	return items, nil
}

func (l *Loader) runOp(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	switch op.kind {
	case srcPkg:
		return l.runPkg(ctx, op)
	case srcPkgFiles:
		return l.runPkgFiles(ctx, op)
	case srcDecode:
		return l.runDecode(ctx, op)
	case srcValue:
		return single(func() (loadItem, error) {
			if op.value == (cue.Value{}) {
				return loadItem{}, fmt.Errorf("cueload: invalid (zero) value in Value source")
			}
			return loadItem{v: op.value}, nil
		})
	case srcSyntax:
		return single(func() (loadItem, error) {
			if op.syntax == nil {
				return loadItem{}, fmt.Errorf("cueload: nil syntax in Syntax source")
			}
			v, err := l.Build(ctx, op.syntax)
			if err != nil {
				return loadItem{}, err
			}
			return loadItem{v: v}, nil
		})
	case srcGo:
		return single(func() (loadItem, error) {
			return loadItem{v: l.empty.FillPath(cue.MakePath(), op.goValue)}, nil
		})
	case srcUnify:
		return l.runUnify(ctx, op)
	case srcConcat:
		return func(yield func(loadItem, error) bool) {
			for _, sub := range op.srcs {
				for item, fatal := range l.run(ctx, sub) {
					if !yield(item, fatal) || fatal != nil {
						return
					}
				}
			}
		}
	case srcAsList:
		return l.runAsList(ctx, op)
	case srcAt:
		return l.mapItems(ctx, op.src, func(item loadItem) (loadItem, error) {
			item.v = l.empty.FillPath(op.path, item.v)
			return item, nil
		})
	case srcLookup:
		return l.mapItems(ctx, op.src, func(item loadItem) (loadItem, error) {
			item.v = item.v.LookupPath(op.path)
			return item, nil
		})
	case srcEval:
		return l.runEval(ctx, op)
	case srcValidate:
		return l.runValidate(ctx, op)
	case srcMap:
		return l.mapItems(ctx, op.src, func(item loadItem) (loadItem, error) {
			v, err := op.mapFunc(ctx, item.v)
			item.v = v
			item.err = err
			return item, nil
		})
	}
	return func(yield func(loadItem, error) bool) {
		yield(loadItem{}, fmt.Errorf("cueload: invalid Source"))
	}
}

// single adapts a one-value computation to a stream.
func single(f func() (loadItem, error)) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		item, err := f()
		yield(item, err)
	}
}

// mapItems transforms each successful item of src with f, passing
// per-item errors through untransformed. A non-nil error from f is
// structural.
func (l *Loader) mapItems(ctx context.Context, src *sourceOp, f func(loadItem) (loadItem, error)) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		for item, fatal := range l.run(ctx, src) {
			if fatal != nil {
				yield(loadItem{}, fatal)
				return
			}
			if item.err != nil {
				if !yield(item, nil) {
					return
				}
				continue
			}
			item, err := f(item)
			if !yield(item, err) || err != nil {
				return
			}
		}
	}
}

// runPkg yields one value per package matched by the pattern. Load
// and build errors of individual packages flow as per-item errors.
func (l *Loader) runPkg(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		paths, err := l.expandPattern(op.pattern)
		if err != nil {
			yield(loadItem{}, err)
			return
		}
		byPath, err := l.ensurePackages(ctx, paths)
		if err != nil {
			yield(loadItem{}, err)
			return
		}
		seen := make(map[*Package]bool)
		for _, p := range paths {
			pkg := byPath[p]
			if pkg == nil || seen[pkg] {
				continue
			}
			seen[pkg] = true
			item := loadItem{
				origin:    Origin{Package: pkg},
				hasOrigin: true,
			}
			v, err := pkg.Value(ctx)
			if err != nil {
				if ctx.Err() != nil {
					yield(loadItem{}, err)
					return
				}
				item.err = err
			} else {
				item.v = v
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// runPkgFiles yields the single value of the ad-hoc package assembled
// from the given files.
func (l *Loader) runPkgFiles(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return single(func() (loadItem, error) {
		if len(op.files) == 0 {
			return loadItem{}, fmt.Errorf("cueload: no files in PkgFiles source")
		}
		syntax, _, err := l.parseCUEFiles(op.files)
		if err != nil {
			return loadItem{}, err
		}
		w, err := l.buildFiles(ctx, "_", syntax)
		if err != nil {
			return loadItem{}, err
		}
		return loadItem{v: l.newValue(w)}, nil
	})
}

// runDecode yields one value per document of the file. Decoding
// problems flow as per-item errors: the affected file's document
// stream ends, but an enclosing Concat continues.
func (l *Loader) runDecode(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		f := op.files[0]
		for doc, err := range l.Decode(ctx, f) {
			if err != nil {
				yield(loadItem{
					origin:    Origin{File: f, Index: doc.Index},
					hasOrigin: true,
					err:       err,
				}, nil)
				return
			}
			item := loadItem{
				origin:    Origin{File: f, Index: doc.Index},
				hasOrigin: true,
			}
			v, err := doc.Value(ctx)
			if err != nil {
				if ctx.Err() != nil {
					yield(loadItem{}, err)
					return
				}
				item.err = err
			} else {
				item.v = v
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// runUnify implements the broadcast rule: at most one operand may
// denote more than one value; the others are unified into each of its
// values.
func (l *Loader) runUnify(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		if len(op.srcs) == 0 {
			yield(loadItem{}, fmt.Errorf("cueload: unify of no sources"))
			return
		}
		streams := make([][]loadItem, len(op.srcs))
		plural := -1
		for i, sub := range op.srcs {
			items, err := l.materialize(ctx, sub)
			if err != nil {
				yield(loadItem{}, err)
				return
			}
			if len(items) == 0 {
				yield(loadItem{}, fmt.Errorf("cueload: unify operand %d (%s) denotes no values", i, sub))
				return
			}
			if len(items) > 1 {
				if plural >= 0 {
					yield(loadItem{}, fmt.Errorf("cueload: unify has more than one plural operand: %s and %s", op.srcs[plural], sub))
					return
				}
				plural = i
			}
			streams[i] = items
		}
		// An error in a broadcast (singular) operand poisons every
		// result; report it as structural.
		for i, items := range streams {
			if i != plural && items[0].err != nil {
				yield(loadItem{}, fmt.Errorf("cueload: unify operand %d (%s): %w", i, op.srcs[i], items[0].err))
				return
			}
		}
		combine := func(item loadItem, driver int) loadItem {
			v := item.v
			for i, items := range streams {
				if i == driver {
					continue
				}
				v = v.Unify(items[0].v)
			}
			item.v = v
			return item
		}
		if plural < 0 {
			// All operands denote one value.
			item := combine(streams[0][0], 0)
			if !item.hasOrigin {
				for _, items := range streams[1:] {
					if items[0].hasOrigin {
						item.origin, item.hasOrigin = items[0].origin, true
						break
					}
				}
			}
			yield(item, nil)
			return
		}
		for _, item := range streams[plural] {
			if item.err != nil {
				if !yield(item, nil) {
					return
				}
				continue
			}
			if !yield(combine(item, plural), nil) {
				return
			}
		}
	}
}

// runAsList collects all values of the operand into one CUE list. Any
// per-item error in the operand is structural: a list cannot represent
// a missing element.
func (l *Loader) runAsList(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return single(func() (loadItem, error) {
		items, err := l.materialize(ctx, op.src)
		if err != nil {
			return loadItem{}, err
		}
		lit := &adt.ListLit{}
		for i, item := range items {
			if item.err != nil {
				return loadItem{}, fmt.Errorf("cueload: list element %d: %w", i, item.err)
			}
			w, verr, fatal := l.forceVertex(ctx, item.v)
			if fatal != nil {
				return loadItem{}, fatal
			}
			_ = verr // an error value is a valid (bottom) list element
			lit.Elems = append(lit.Elems, w)
		}
		n := &adt.Vertex{}
		n.AddConjunct(adt.MakeRootConjunct(nil, lit))
		return loadItem{v: l.newValue(n)}, nil
	})
}

// runEval evaluates the expression in the scope of each value. The
// input values are forced at load time: the expression is resolved
// within the evaluated result.
func (l *Loader) runEval(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return l.mapItems(ctx, op.src, func(item loadItem) (loadItem, error) {
		w, verr, fatal := l.forceVertex(ctx, item.v)
		if fatal != nil {
			return loadItem{}, fatal
		}
		if verr != nil {
			item.err = verr
			return item, nil
		}
		item.v = l.evalExpr(ctx, w, op)
		return item, nil
	})
}

// evalExpr compiles and resolves op.expr in the scope of the vertex w,
// mirroring v1's Context.BuildExpr with a Scope.
func (l *Loader) evalExpr(ctx context.Context, w *adt.Vertex, op *sourceOp) cue.Value {
	astutil.ResolveExpr(op.expr, func(pos token.Pos, msg string, args ...interface{}) {})
	conj, cerr := compile.Expr(&compile.Config{Scope: vertexScope{w}}, l.rt, "_", op.expr)
	if cerr != nil {
		return l.newValue(errVertex(cerr))
	}
	opCtx := l.newOpContext(ctx, w)
	defer opCtx.FlushStats()
	return l.newValue(adt.Resolve(opCtx, conj))
}

// vertexScope adapts a vertex chain to the compiler's Scope interface.
type vertexScope struct {
	v *adt.Vertex
}

func (s vertexScope) Vertex() *adt.Vertex { return s.v }

func (s vertexScope) Parent() compile.Scope {
	if s.v.Parent == nil {
		return nil
	}
	return vertexScope{s.v.Parent}
}

// runValidate checks each data value against the schema, yielding the
// unified values; validation failures flow as per-item errors.
func (l *Loader) runValidate(ctx context.Context, op *sourceOp) iter.Seq2[loadItem, error] {
	return func(yield func(loadItem, error) bool) {
		schemaItems, err := l.materialize(ctx, op.schema)
		if err != nil {
			yield(loadItem{}, err)
			return
		}
		if len(schemaItems) != 1 {
			yield(loadItem{}, fmt.Errorf("cueload: validation schema (%s) denotes %d values, want 1", op.schema, len(schemaItems)))
			return
		}
		if err := schemaItems[0].err; err != nil {
			yield(loadItem{}, fmt.Errorf("cueload: loading validation schema (%s): %w", op.schema, err))
			return
		}
		schema := schemaItems[0].v
		var opts []cue.Option
		if op.vopts.concrete {
			opts = append(opts, cue.Concrete(true))
		}
		for item, fatal := range l.run(ctx, op.src) {
			if fatal != nil {
				yield(loadItem{}, fatal)
				return
			}
			if item.err != nil {
				if !yield(item, nil) {
					return
				}
				continue
			}
			unified := item.v.Unify(schema)
			verr := unified.Validate(ctx, opts...)
			if ctx.Err() != nil {
				yield(loadItem{}, ctx.Err())
				return
			}
			item.v = unified
			item.err = verr
			if !yield(item, nil) {
				return
			}
		}
	}
}
