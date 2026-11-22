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

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuedebug"
)

// Runtime defines an interface for low-level representation conversion and
// lookup.
type Runtime interface {
	// StringIndexer allows for converting string labels to and from a
	// canonical numeric representation.
	StringIndexer

	// LoadBuiltin loads a unique Vertex associated with a given builtin
	// (standard library) import path. It returns nil if no builtin for
	// this import path could be found.
	LoadBuiltin(importPath string) *Vertex

	// LoadInstance loads a unique Vertex associated with the given build
	// instance. It returns nil if no such instance has been compiled.
	LoadInstance(inst *build.Instance) *Vertex

	// StoreType associates a finalized CUE Vertex with a Go type.
	StoreType(t reflect.Type, v *Vertex)

	// LoadType retrieves a previously stored CUE Vertex for a given Go
	// type if available.
	LoadType(t reflect.Type) (*Vertex, bool)

	// ConfigureOpCtx configures the [*OpContext] with details such as
	// evaluator version, debug options etc.
	ConfigureOpCtx(ctx *OpContext)

	// FormatNode prints n for use in errors or debugging.
	FormatNode(n Node) string
}

var contextGeneration atomic.Uint64

// NewContext creates an operation context.
func NewContext(r Runtime, v *Vertex) *OpContext {
	if r == nil {
		panic("nil Runtime")
	}

	ctx := &OpContext{
		opID:        contextGeneration.Add(1),
		Runtime:     r,
		vertex:      v,
		taskContext: schedConfig,
	}
	r.ConfigureOpCtx(ctx)
	ctx.stats.EvalVersion = ctx.Version
	if v != nil {
		ctx.e = &Environment{Up: nil, Vertex: v}
	}
	if ctx.LogEval > 0 {
		ctx.Logf(v, "New context at opID %d", ctx.opID)
	}
	return ctx
}

// IsValidator reports whether the context is being evaluated
// as part of a validator function call.
func (c *OpContext) IsValidator() bool {
	return c.isValidator
}

// An OpContext holds context associated with an on-going CUE
// evaluation. It functions both as an optimized memory store,
// amortizing allocations during an evaluation, and as a record of the
// current state within an evaluation.
//
// It should only be used on values that are created with the Runtime
// with which an OpContext is created.
//
// An OpContext is not goroutine safe and only one goroutine may use an
// OpContext at a time.
//
// An OpContext is typically used for an entire operation with CUE values,
// such as any call to exported Go APIs like methods on [cue.Value].
//
// An OpContext stores:
// - errors encountered during the evaluation
// - the current vertex and its parents
// - statistics on evaluation operations
//
// The recorded set of errors is added to by calls to [OpContext.AddErr],
// [OpContext.AddErr], [OpContext.AddErrf], and in general
// any other operation that encounters an error.
//
// The current vertex is modified by calling [OpContext.PushArc], which
// must be balanced by a corresponding call to [OpContext.PopArc].
//
// The entire state, including recorded errors and the current vertex, can be
// reset by calling [OpContext.PushState], which must be balanced by a
// corresponding call to [OpContext.PopState], causing the original
// errors and vertex to be restored.
type OpContext struct {
	Runtime

	cuedebug.Config
	Version internal.EvaluatorVersion // Copied from Runtime

	taskContext

	nest int

	// used in typocheck.go
	nextDefID    defID         // next available defID
	containments []containment // parent relations

	// sawOpened is set once a reference opened with the postfix ... operator
	// was added to any node; see [nodeContext.underOpened].
	sawOpened bool

	// containsDefIDVisited is scratch space for [nodeContext.containsDefIDRec]:
	// the replacement sources it has already explored within a single
	// [nodeContext.containsDefID] call. The buffer is reused across calls.
	//
	// It holds at most one entry per replaceID of the node, and that count
	// stays small: the largest seen across the test corpus is single digits,
	// and [nodeContext.containsDefID] treats more than 15 as unusual enough
	// to memoize. A linear search is therefore cheaper than hashing. If
	// the lengths ever grow, the slice can be kept sorted and searched with
	// binary search instead.
	containsDefIDVisited []defID

	// [token.Pos] interning for containments to reduce memory usage,
	// given that millions of elements in [OpContext.containments]
	// can share the same position. [uint32] is 4 bytes,
	// whereas [token.Pos] is 16 bytes with alignment.
	positionTable []token.Pos          // unique positions
	positionIndex map[token.Pos]uint32 // reverse lookup for interning

	// disjunctBuffer is reused when constructing [envDisjunct.disjuncts].
	disjunctBuffer []disjunct

	stats        stats.Counts
	freeListNode *nodeContext

	// pendingConstraints maps the pattern constraints whose copies are
	// being evaluated by [Vertex.PatternConstraint] to those copies.
	pendingConstraints map[*Vertex]*Vertex

	e  *Environment
	ci CloseInfo

	// funcAnchors caches the stable anchor vertex for each native CUE
	// function literal, keyed by literal and closure environment (see
	// [envKey] for what identifies an environment). Reusing it across
	// (recursive) calls lets the regular structural cycle detector observe
	// recursion. See [OpContext.funcAnchor].
	funcAnchors map[funcAnchorKey]*Vertex

	// funcCallRefs caches the stable reference to an anchor vertex for each
	// call site, keyed by call expression and anchor vertex. A
	// per-call-site reference keeps the (vertex, source-reference) distinction
	// the structural cycle detector relies on, so that recursion (same call
	// site) is a cycle while nesting (distinct call sites) is not. Each entry
	// holds one reference per set of signature constraints of the called
	// value. See [OpContext.funcCallRef].
	funcCallRefs map[funcCallRefKey][]*FuncCallRef

	// funcCallResults memoizes the finalized, error-free result vertices of
	// native CUE function calls, keyed by call site and the caller
	// environment in which its arguments resolve. A call's result is fully
	// determined by that pair and the callee, so a parameter used N times in
	// a body — or a call nested as an argument and consumed once per use of
	// the enclosing parameter — re-dispatches the same call instead of
	// rebuilding the activation and re-finalizing the body each time.
	// Without this the work of deeply nested calls (e.g. twice(twice(...)))
	// grows exponentially in the nesting depth.
	//
	// Only finalized, error-free results are stored: a call still in progress
	// (in particular one re-entered through recursion, which the structural
	// cycle detector must observe on the shared anchor) has no entry, so
	// memoization never short-circuits cycle detection. See
	// [funcCallResultKey], [funcCallResult] and [FuncValue.call].
	funcCallResults map[funcCallResultKey][]funcCallResult

	// anonParamLabels caches the labels of the synthetic activation arcs
	// that bind the arguments of anonymous positional function parameters,
	// indexed by parameter position. See [anonParamLabel].
	anonParamLabels []Feature

	// admission holds what one outermost declared-compatibility comparison
	// has established about the structs and lists it reached. See
	// [OpContext.admits].
	admission *admissionMemo

	// capturesCompared records the comparisons of the values that two
	// closures of one literal capture that are in progress, and dataPairs the
	// outcome of comparing each pair of captured vertices as data. See
	// [OpContext.sameCaptures]. Both count a comparison in progress as equal,
	// so their outcomes only hold within the outermost comparison.
	capturesCompared map[captureKey]bool
	dataPairs        map[[2]*Vertex]bool

	// funcTypeDepth counts the function types being compared as parameters
	// or results of other function types, which bounds that recursion. See
	// [OpContext.funcTypeAdmits].
	funcTypeDepth int

	// funcTypePairs records the outcome of each pair of function types
	// compared during one outermost comparison, and funcTypeActive the pairs
	// of literals being compared, which a nested comparison of the same
	// literals takes to admit. See [OpContext.funcTypeAdmits].
	funcTypePairs  map[[2]FuncType]bool
	funcTypeActive map[[2]*Function]bool

	// checkingDefaults tracks the function literals, per closure, whose
	// declared parameter defaults are being checked against their constraints
	// (see [Function.checkDefaults]). The check runs outside any call, so a
	// default that calls the function itself would re-enter it; re-entry for
	// a closure already on this set leaves the check undone, and the call
	// that omits the parameter reports the cycle.
	checkingDefaults map[funcAnchorKey]bool

	// funcDefaultsChecked records the closures whose parameter defaults have
	// been checked (see [Function.checkDefaults]), so that the check runs
	// once per closure.
	funcDefaultsChecked map[funcAnchorKey]bool

	// Source node associated with the CUE operation, if any.
	// When nil, created nodes like [Bool] may use sentinels to avoid allocations.
	src ast.Node

	errs      *Bottom
	positions []Node // keep track of error positions

	// vertex is used to determine the path location in case of error. Turning
	// this into a stack could also allow determining the cyclic path for
	// structural cycle errors.
	vertex *Vertex

	// lookupPendingParent is set by [Vertex.lookup] in attemptOnly mode when
	// a field was not found but a parent task may still produce it.
	// [processResolver] clears it before resolving and uses it to requeue,
	// saving and restoring the previous value as resolving may recursively
	// run other resolver tasks.
	lookupPendingParent bool

	// finalizeDepth counts the evaluations in progress which were started
	// via [Vertex.Finalize], [Vertex.Unify], or the CompleteArcs methods.
	finalizeDepth int

	// finalizeID identifies the outermost [Vertex.Finalize] call while it
	// evaluates a vertex, and is zero otherwise. See [Vertex.finalizedBy].
	finalizeID uint64

	// list of vertices that need to be finalized.
	// TODO: remove this again once we have a proper way of detecting references
	// across optional boundaries in hasAncestorV3. We can probably do this
	// with an optional depth counter.
	// See the TODO in unify.go for toFinalize.
	// toFinalize []*Vertex

	// freeScope tracks the nodeContexts that are currently responsible for
	// allocating new inlined vertices. Only nodes within the current scope can
	// refer to any values allocated within an inline node, which means it is
	// safe to reuse the nodeContext when all values have been processed.
	// Values yielded by comprehensions are an exception; see [Vertex.escapes].
	//
	// TODO(mem): we can mark Vertex nodes as being used in sharing and then
	// free them too if they are not used.
	freeScope []*nodeContext

	// These fields are used associate scratch fields for computing closedness
	// of a Vertex. These fields could have been included in StructInfo (like
	// Tomabechi's unification algorithm), but we opted for an indirection to
	// allow concurrent unification.
	//
	// TODO(perf): have two generations: one for each pass of the closedness
	// algorithm, so that the results of the first pass can be reused for all
	// features of a node.
	opID uint64

	// evalDepth indicates the current depth of evaluation. It is used to
	// detect structural cycles and their severity.s
	evalDepth int

	// holdID is a unique identifier for the current "hole", a choice of
	// disjunct to be made when processing disjunctions.
	holeID int

	// nextCompID counts per-firing comprehension identifiers. Each call to
	// [nodeContext.processComprehension] allocates one ID and stamps it on
	// every per-yield Environment so toposort can group sibling body decls
	// (see [Environment.CompID]). 0 is reserved for "not from a comprehension".
	nextCompID uint32

	// inDetached indicates that inline structs evaluated in the current context
	// should never be shared. This is the case, for instance, with the source
	// for the for clause in a comprehension.
	inDetached int

	// inValidator defines whether full evaluation need to be enforced, for
	// instance when comparing against bottom.
	inValidator int

	// The current call is a validator. A builtin may return a boolean false
	// along with an error message describing a validation error. If the latter
	// is wrapped in an internal.ValidationError, it will only be interpreted
	// as an error if this is true.
	// TODO: strictly separate validators and functions.
	isValidator bool

	overlays []overlayFrame

	// ==== Debugging ====
	logID int // sequence number for log messages

	currentDisjunctionID int // sequence number for call to processDisjunctions

	disjunctStack []disjunctInfo // stack of disjunct IDs

	// altPath, if non-empty, provides an alternative path for errors. This is
	// necessary to get the right path for incomplete errors in the presence of
	// structure sharing.
	altPath []*Vertex // stack of selectors

	// importInstances memoizes a private, per-evaluation instance of each
	// imported package root this context references, so concurrent evaluations
	// do not share the runtime's mutable cached template.
	// See [OpContext.importInstance].
	importInstances map[*Vertex]*Vertex

	// letCache holds let results per environment. It is per context as
	// contexts share environments, and not in [nodeContext] as one result
	// serves the whole environment.
	letCache map[envCacheKey]letCacheEntry
}

// envCacheKey uses [envKey] rather than the environment pointer, as the
// per-field copies of a pattern constraint's environment differ only in
// [Environment.DynamicLabel], which a let there cannot refer to.
type envCacheKey struct {
	env envKey
	cacheKey
}

func (c *OpContext) CloseInfo() CloseInfo { return c.ci }

func (c *OpContext) Pos() token.Pos {
	if c.src == nil {
		return token.NoPos
	}
	return c.src.Pos()
}

func (c *OpContext) Source() ast.Node {
	return c.src
}

func (c *OpContext) pos() token.Pos {
	if c.src == nil {
		return token.NoPos
	}
	return c.src.Pos()
}

func (c *OpContext) spawn(node *Vertex) *Environment {
	return spawn(c.e, node)
}

func spawn(env *Environment, node *Vertex) *Environment {
	return &Environment{
		Up:     env,
		Vertex: node,
	}
}

func (c *OpContext) Env(upCount int32) *Environment {
	return c.e.up(c, upCount)
}

func (c *OpContext) relNode(upCount int32) *Vertex {
	return c.derefNode(c.e.up(c, upCount))
}

// derefNode returns the dereferenced vertex of e, ensuring it is at least
// partially evaluated.
func (c *OpContext) derefNode(e *Environment) *Vertex {
	v := e.DerefVertex(c)
	c.unify(v, Flags{
		status:    partial,
		condition: allKnown,
		mode:      ignore,
	})
	return v
}

func (c *OpContext) relLabel(upCount int32) Feature {
	// locate current label.
	e := c.e.up(c, upCount)
	return e.DynamicLabel
}

func (c *OpContext) concreteIsPossible(op Op, x Expr) bool {
	if !AssertConcreteIsPossible(op, x) {
		// No need to take position of expression.
		c.AddErr(c.NewPosf(token.NoPos,
			"invalid operand %s ('%s' requires concrete value)", x, op))
		return false
	}
	return true
}

// AssertConcreteIsPossible reports whether the given expression can evaluate to a concrete value.
func AssertConcreteIsPossible(op Op, x Expr) bool {
	switch v := x.(type) {
	case *Bottom:
	case *BoundExpr:
		return false
	case Value:
		return v.Concreteness() == Concrete
	}
	return true
}

// HasErr reports whether any error was reported, including whether value
// was incomplete.
func (c *OpContext) HasErr() bool {
	return c.errs != nil
}

func (c *OpContext) Err() *Bottom {
	b := c.errs
	c.errs = nil
	return b
}

// ResolveExists reports whether the field reference, selector, or index
// expression x refers to a regular member that is present. It returns an error
// for a non-reference argument, a missing intermediate component, or when the
// answer would be order-dependent (a cycle). name labels the builtin in error
// messages.
func (c *OpContext) ResolveExists(x Expr, name string) (bool, *Bottom) {
	env := c.Env(0)
	var parent *Vertex
	var closed bool // parent is the argument of an inline close()
	var opened bool // parent is reached through an opened reference
	var sel Feature
	var pos token.Pos

	switch r := x.(type) {
	case *FieldReference:
		// A variable bound by a comprehension clause is compiled as a field
		// reference, but it is not a field: like a let, it names a value
		// rather than a member that may or may not be present.
		if r.Src != nil {
			switch r.Src.Scope.(type) {
			case *ast.ForClause, *ast.TryClause:
				return false, c.NewErrf("%s: argument must be a field reference, selector, or index expression", name)
			}
		}
		// A field reference is a selection on the vertex of the scope that
		// declares the field. Answer it as such, so that it shares the
		// presence logic and the stability post-check of the selector form.
		// Resolving the reference itself would compute the member's value,
		// which presence does not depend on.
		sel = r.Label
		if r.Src != nil {
			pos = r.Src.Pos()
		}
		parent = env.up(c, r.UpCount).DerefVertex(c)

	case *SelectorExpr:
		sel = r.Sel
		if r.Src != nil {
			pos = r.Src.Sel.Pos()
		}
		var err *Bottom
		if parent, closed, opened, err = c.existsSubject(env, x, r.X, pos, name); err != nil {
			return false, err
		}

	case *IndexExpr:
		if r.Src != nil {
			pos = r.Src.Index.Pos()
		}
		idx, _ := c.Evaluate(c.Env(0), r.Index)
		if b, isErr := idx.(*Bottom); isErr {
			// Propagate by flavor: a permanent error in the index operand is
			// a broken question; an incomplete one is an unformed question.
			return false, b
		}
		// A defaulted index is its default, as for an index expression
		// outside a call.
		idx = ConcreteDefault(idx)
		if idx.Concreteness() != Concrete {
			return false, &Bottom{
				Code: IncompleteError,
				Err:  c.NewPosf(pos, "%s: non-concrete index %s", name, idx),
			}
		}
		sel = c.Label(r.Index, idx)
		if c.HasErr() {
			return false, c.Err()
		}
		var err *Bottom
		if parent, closed, opened, err = c.existsSubject(env, x, r.X, pos, name); err != nil {
			return false, err
		}

	default:
		return false, c.NewErrf("%s: argument must be a field reference, selector, or index expression", name)
	}

	return c.existsMember(env, x, parent, closed, opened, sel, pos, name)
}

// existsSubject resolves the subject value of the selector or index
// expression x for [OpContext.ResolveExists]: the vertex whose members are
// asked about. It also reports two things that are recorded on a vertex
// which is not the one returned: whether the subject is the result of an
// inline close(), which records its closedness on a wrapper vertex, and
// whether it shares a vertex through a reference opened with the postfix ...
// operator.
func (c *OpContext) existsSubject(env *Environment, x, value Expr, pos token.Pos, name string) (_ *Vertex, closed, opened bool, _ *Bottom) {
	// Resolve the peer cooperatively, the way regular selector resolution
	// does (see [SelectorExpr.resolve] and [OpContext.node]): only the peer's
	// field set is needed to answer an existence question, and mode yield
	// lets the scheduler suspend the current task while the peer is still
	// being computed, re-running it when the peer's field set is known.
	// Forcing the peer with [OpContext.Evaluate] instead would make tasks of
	// the peer, such as a builtin call awaiting one of its arguments, fail
	// with a spurious cycle error whenever evaluation merely entered the
	// graph through a not-yet-computed node.
	s := c.PushState(env, x.Source())
	peerV, arc := c.unifyNodeRef(value, Flags{
		status:    partial,
		condition: needFieldSetKnown,
		mode:      yield,
	})
	// Errors accumulated while obtaining the peer's field set may stem from
	// the values of members already computed by the regular evaluation order.
	// Presence is independent of member values, so such errors are ignored
	// when the peer itself resolves to a usable vertex below; cycle errors
	// always propagate, as the answer would be order-dependent.
	peerErr := c.PopState(s)
	if peerErr != nil && peerErr.Code == CycleError {
		return nil, false, false, peerErr
	}
	// An inline close() records its closedness on a wrapper vertex, which
	// resolving the default and unwrapping below discard. A vertex that
	// shares another through an opened reference records that it is open,
	// and resolving the peer above has dereferenced it already. A subject
	// that is itself an opened expression, as in (#D...).a, is open.
	if _, ok := value.(*OpenExpr); ok {
		opened = true
	} else {
		opened = refOpened(arc)
	}
	for w, _ := peerV.(*Vertex); w != nil; w, _ = w.BaseValue.(*Vertex) {
		closed = closed || w.ClosedNonRecursive
	}
	// A definition closes again what a field within it shares. Like an
	// inline close(), it then admits the declared fields and those that
	// match a pattern only.
	closed = closed || refReclosed(arc)
	// A disjunction with a default is navigated through its default, which
	// finalization selects. Without a default the subject's field set is
	// genuinely undetermined, so a presence question about it is unformed:
	// committing false would claim a member is absent that some disjunct may
	// have (CP-E8).
	w, unresolved := resolveDefault(peerV)
	if unresolved != nil {
		return nil, false, false, c.unresolvedExistsSubject(peerV, pos, name)
	}
	peerV = w
	if w := Unwrap(peerV); !isCyclePlaceholder(w) {
		peerV = w
	}
	if vtx, ok := peerV.(*Vertex); !ok || vtx.state == nil || !vtx.state.scheduler.running {
		// The peer is not actively being computed: forcing it cannot
		// produce a spurious cycle and acts as the driver for values that
		// are not driven by the regular evaluation order, such as let
		// values and comprehension bindings. Force only up to the field
		// set: presence is a property of the parent's structure, so the
		// members' values — including a broken value of the very member
		// being asked about — must not be computed, let alone propagated.
		s := c.PushState(env, x.Source())
		forced := c.unifyNode(value, Flags{
			status:    partial,
			condition: needFieldSetKnown,
			mode:      finalize,
		})
		if b := c.PopState(s); b != nil && peerErr == nil {
			peerErr = b
		}
		peerV = forced
	}
	if b, isErr := peerV.(*Bottom); isErr {
		vtx, ok := b.Value.(*Vertex)
		if !b.ChildError || !ok {
			return nil, false, false, b
		}
		// A member's broken value surfaces as its container's own bottom,
		// but the container's field set is intact. Presence is independent
		// of member values, so answer from the recovered container vertex.
		peerV = vtx
	}
	parent, ok := peerV.(*Vertex)
	if !ok {
		if peerErr != nil {
			return nil, false, false, peerErr
		}
		return nil, false, false, c.NewErrf("%s: cannot index non-struct/list value", name)
	}

	// Follow defaults: a selection on a disjunction with a default picks the
	// default disjunct, as the old `!= _|_` validator did via
	// [OpContext.getDefault]. Only descend when the value is actually a
	// disjunction; otherwise DefaultDisjunct() can produce a stale copy.
	// The default is taken as it is: an open list stays open, so that an
	// index beyond it is absent but possible, as for a list that is not
	// the default of a disjunction.
	if _, isDisjunction := parent.BaseValue.(*Disjunction); isDisjunction {
		parent = parent.DefaultDisjunct()
	}
	if _, isDisjunction := parent.BaseValue.(*Disjunction); isDisjunction {
		// Unresolved even after applying defaults.
		return nil, false, false, c.unresolvedExistsSubject(parent, pos, name)
	}
	parent = parent.DerefValue()

	// The subject may still be in progress, as when it is declared after the
	// call: so far only its field set is known, and its base value is a
	// placeholder. An error of the subject itself is then recorded in its
	// state only. Report it as resolving the reference does for a subject
	// that was evaluated first, so that the answer does not depend on the
	// order of evaluation.
	inProgress := isCyclePlaceholder(parent.BaseValue)
	if n := parent.state; inProgress && n != nil && n.errs != nil && !n.errs.IsIncomplete() {
		return nil, false, false, n.errs
	}
	if _, isBottom := parent.BaseValue.(*Bottom); !isBottom || inProgress {
		if k := parent.Kind(); k&(StructKind|ListKind) == 0 {
			// The parent's kind admits no members at all, so the answer can
			// never become true; a committed false would be misleading. A
			// bottom-based vertex is exempt: a recovered child-error
			// container is a composite whose kind reads as bottom. The
			// placeholder of a subject in progress is not such a container:
			// its kind is read from its state.
			return nil, false, false, c.NewErrf("%s: value of type %s can never have members", name, k)
		}
	}
	return parent, closed, opened, nil
}

// refOpened reports whether arc, the vertex that a reference resolved to
// before it is dereferenced, stands for a struct opened with the postfix ...
// operator: it shares another vertex through an opened reference. Such a
// vertex records that it is open, as the vertex it shares is not: see
// [Vertex.OpenedShared]. Evaluating the reference dereferences the vertex,
// so that this is lost.
//
// A definition closes again what is opened within it; see [Vertex.Accept].
//
// This reads what the vertices record, whichever OpContext evaluated them.
func refOpened(arc *Vertex) bool {
	return arc != nil && arc.sharesOpened() && enclosingDef(arc) == nil
}

// refReclosed reports whether arc, the vertex that a reference resolved to
// before it is dereferenced, shares another vertex from within a definition.
// The definition closes the shared vertex again, whatever that vertex
// records about itself, such as [Vertex.Opened]. Evaluating the reference
// dereferences arc, so that this is lost as well.
func refReclosed(arc *Vertex) bool {
	return arc != nil && arc.DerefValue() != arc && enclosingDef(arc) != nil
}

// unresolvedExistsSubject reports that the subject v of an existence question
// is a disjunction without a default. The subject's field set is genuinely
// undetermined, so the question is unformed: an incomplete error. Such a
// configuration is itself erroneous at export.
func (c *OpContext) unresolvedExistsSubject(v Value, pos token.Pos, name string) *Bottom {
	b := &Bottom{
		Code: IncompleteError,
		Err:  c.NewPosf(pos, "%s: unresolved disjunction", name),
	}
	if vtx, ok := v.(*Vertex); ok {
		b.Node = vtx
	}
	return b
}

// existsPossible reports whether the absent member sel of parent could still
// be added by a legal configuration, for [OpContext.ResolveExists].
// inlineClose reports whether parent is the argument of an inline close(),
// and opened whether it is reached through an opened reference; see
// [OpContext.existsSubject].
func (c *OpContext) existsPossible(parent *Vertex, inlineClose, opened bool, sel Feature) bool {
	if opened && sel.IsString() && parent.Kind()&StructKind != 0 {
		// The postfix ... operator opens the struct it is applied to.
		return true
	}
	if !parent.Accept(c, sel) {
		return false
	}
	if !sel.IsString() {
		return true
	}
	if inlineClose {
		// An inline close() admits the declared fields of its argument and
		// those matching its patterns only.
		return parent.Lookup(sel) != nil || parent.accept(c, sel)
	}
	// Accept reads whether a struct is closed from what is recorded on it,
	// which says nothing in two cases. Both read as open. Judge them by what
	// closes them instead, so that the answer depends neither on the order
	// of evaluation nor on the health of other members.
	n := parent.state
	closed := parent.ClosedRecursive || parent.ClosedNonRecursive
	open := parent.HasEllipsis
	switch b, _ := parent.BaseValue.(*Bottom); {
	case b != nil && !isCyclePlaceholder(b):
		// A struct that holds a broken member holds that error as its base
		// value. What closes it is recorded on it as for any struct.
		if !b.ChildError {
			return true
		}

	case n != nil && parent.status != finalized:
		// A struct that is still in progress: one declared after the call,
		// or one that the call is evaluated within. A struct is marked as
		// closed, if it unifies with a definition, and its ellipsis is
		// cleared, if it does not open it, when it is finalized. Judge the
		// struct as it will be then.
		if parent.Kind() != StructKind {
			return true
		}
		closed = closed || (n.isDef && (n.hasStruct || !n.hasTop))
		if closed && (open || n.underOpened()) {
			// An ellipsis only opens the struct it is part of, so it need
			// not open a struct that something else closes, as in
			// #D & {...}. The postfix ... operator, in turn, opens a
			// struct that is marked as closed.
			open = n.peekOpenByEllipsis()
		}

	default:
		return true
	}
	if !closed || open || parent.Lookup(sel) != nil {
		return true
	}
	if n != nil {
		for _, d := range n.disjunctions {
			for _, x := range d.disjuncts {
				if mayDeclare(x.expr, sel) {
					return true
				}
			}
		}
	}
	return parent.accept(c, sel)
}

// existsMember reports whether sel is a regular member of parent that is
// present, for [OpContext.ResolveExists].
func (c *OpContext) existsMember(env *Environment, x Expr, parent *Vertex, closed, opened bool, sel Feature, pos token.Pos, name string) (bool, *Bottom) {
	s := c.PushState(env, x.Source())

	c.inValidator++
	defer func() { c.inValidator-- }()

	flags := Flags{
		status:    conjuncts,
		condition: needTasksDone,
		mode:      finalize,
	}
	var v Value
	arc := c.lookup(parent, pos, sel, flags)
	if arc != nil {
		// A regular member exists regardless of its value: presence is
		// monotone, as conjuncts can only be added. Answer before completing
		// the arc: completion computes the arc's value, which may force
		// nodes that are still legitimately waiting on other tasks and
		// thereby trigger premature cycle breaking.
		if arc.ArcType == ArcMember {
			c.PopState(s)
			return true, nil
		}
		arc.CompleteArcsShallow(c)
		v = arc.DerefNonShared()
		if arc.ArcType == ArcMember {
			c.PopState(s)
			return true, nil
		}

	}
	if c.HasErr() {
		v = c.errs
	}

	u, _ := c.getDefault(v)
	c.PopState(s)

	if v, ok := Unwrap(u).(*Bottom); ok {
		switch v.Code {
		case CycleError:
			return false, v
		case IncompleteError:
			// The responsible node is actively being computed and still has
			// value-producing tasks pending; suspend until its value is known
			// and re-run, as [OpContext.validate] does for comparisons against
			// bottom. Unlike the propagation below, which relies on the
			// current task being retried by its own node, suspension also
			// reschedules the current task when it waits on a *different*
			// node, whose completion would otherwise not retrigger it.
			if sched := pendingValueScheduler(v.Node); sched != nil {
				if t := c.current(); t != nil {
					t.waitFor(sched, valueKnown)
					sched.yield()
					panic("unreachable")
				}
			}

			c.verifyNonMonotonicResult(env, x, true)

			// For a SelectorExpr or IndexExpr `peer.field`, identify the peer
			// vertex (distinct from the caller). If the peer is
			// mid-evaluation with a pending comp that could yet
			// add this field, propagate so we don't commit to a
			// stale "field absent" answer.
			caller := env.DerefVertex(c)
			if parent != caller {
				if c.hasPendingComp(parent, sel) {
					return false, v
				}
			}
		}
	}

	// The member is absent. Distinguish "absent but possible" from "can
	// never exist": an undeclared label on a closed struct, or an
	// out-of-range index of a closed list, makes the question permanently
	// false, which is reported as an error rather than a misleading false.
	// Possibility is judged structurally: declared optional and required
	// fields, pattern-admitted labels, and open parents all yield false.
	// Hidden fields and definitions are always accepted.
	if !c.existsPossible(parent, closed, opened, sel) {
		// Say what is wrong with the question where the kind of the parent
		// tells: a parent that was still in progress when the subject was
		// resolved has its kind by now. The kind of a parent that holds a
		// broken member reads as bottom, and tells nothing.
		switch k := parent.Kind(); {
		case k == BottomKind:
		case k&(StructKind|ListKind) == 0:
			return false, c.NewErrf("%s: value of type %s can never have members", name, k)
		case sel.IsInt() && k&ListKind == 0:
			return false, c.NewErrf("%s: invalid index %d (found %s, want list)", name, sel.Index(), k)
		case sel.IsString() && k&StructKind == 0:
			return false, c.NewErrf("%s: invalid field %v (found %s, want struct)", name, sel, k)
		}
		if sel.IsInt() {
			return false, c.NewErrf("%s: index %d out of range", name, sel.Index())
		}
		return false, c.NewErrf("%s: field %v not allowed by closed struct", name, sel)
	}

	return false, nil
}

// EvaluateReflect evaluates x on behalf of a reflection builtin that commits
// definite answers for values that are incomplete at the configuration's
// fixpoint. It returns:
//
//   - (v, nil) if x evaluates to the non-bottom value v;
//   - (nil, b) if evaluation must fail with b: a permanent error, which
//     reflection builtins other than isValid propagate, or a cycle;
//   - (nil, nil) if x is incomplete at the fixpoint. The caller commits its
//     definite answer for a not-yet-determined value; a post-check is
//     registered to verify that the expression still evaluates to a bottom
//     once the enclosing node finalizes.
//
// If the value's node is still actively being computed, the current task is
// suspended and re-run when the value is known, rather than committing an
// answer early.
func (c *OpContext) EvaluateReflect(x Expr, name string) (Value, *Bottom) {
	env := c.Env(0)
	v, _ := c.Evaluate(env, x)
	b, ok := v.(*Bottom)
	if !ok {
		return v, nil
	}
	switch b.Code {
	case CycleError:
		return nil, b
	case IncompleteError:
		if sched := pendingValueScheduler(b.Node); sched != nil {
			if t := c.current(); t != nil {
				t.waitFor(sched, valueKnown)
				sched.yield()
				panic("unreachable")
			}
		}
		c.verifyNonMonotonicResult(env, x, true)
		return nil, nil
	default:
		return nil, b
	}
}

// pendingValueScheduler returns the scheduler of n if n is actively being
// computed and still has value-producing tasks pending, meaning a task may
// wait on it for valueKnown. It returns nil otherwise. Requiring a running
// scheduler avoids deadlocking on a node that nobody else will trigger — a
// genuine cycle.
func pendingValueScheduler(n *Vertex) *scheduler {
	if n == nil {
		return nil
	}
	ns := n.state
	if ns != nil && ns.running &&
		!ns.meets(valueKnown) && ns.provided&valueKnown != 0 {
		return &ns.scheduler
	}
	return nil
}

// hasPendingComp reports whether a task of v that is still pending could yet
// make the member sel of v present: any non-success task remaining, such as a
// comprehension or a builtin call awaiting an input. A "doesn't exist" answer
// right now would then be order-dependent.
//
// A pushed-down comprehension is the exception: it can only contribute to the
// arcs it pre-created, which list it in their parent tasks (see
// [task.isPushedDownComp]). It cannot make any other member present.
//
// Tasks on the current task stack do not count either: they are evaluating
// this very question, as when v is a lexical ancestor reached through a field
// reference, and cannot complete before it is answered. The committed answer
// is verified by the post-check registered for it.
//
// Registered post-checks do not count. They are never cleared, so counting
// them would keep the answer incomplete forever once any non-monotonic guard
// ran in v. A comprehension whose guard committed under an incomplete value
// has settled: if its guard later flips, v's own post-check reports the
// cycle.
func (c *OpContext) hasPendingComp(v *Vertex, sel Feature) bool {
	if v == nil || v.state == nil {
		return false
	}
	var arcState *nodeContext
	for _, a := range v.Arcs {
		if a.Label == sel {
			arcState = a.state
			break
		}
	}
	for _, t := range v.state.tasks {
		if t.state == taskSUCCESS {
			continue
		}
		if t.isPushedDownComp() &&
			(arcState == nil || !slices.Contains(arcState.parentTasks, t)) {
			continue
		}
		if t.state == taskRUNNING && slices.Contains(c.stack, t) {
			continue
		}
		return true
	}
	return false
}

func (c *OpContext) addErrf(code ErrorCode, pos token.Pos, msg string, args ...interface{}) {
	err := c.NewPosf(pos, msg, args...)
	c.addErr(code, err)
}

func (c *OpContext) addErr(code ErrorCode, err errors.Error) {
	c.AddBottom(&Bottom{
		Code: code,
		Err:  err,
		Node: c.vertex,
	})
}

// AddBottom records an error in OpContext.
func (c *OpContext) AddBottom(b *Bottom) {
	c.errs = CombineErrors(c.src, c.errs, b)
}

// AddErr records an error in OpContext. It returns errors collected so far.
func (c *OpContext) AddErr(err errors.Error) *Bottom {
	if err != nil {
		c.AddBottom(&Bottom{
			Err:  err,
			Node: c.vertex,
		})
	}
	return c.errs
}

// NewErrf creates a *Bottom value and returns it. The returned uses the
// current source as the point of origin of the error.
func (c *OpContext) NewErrf(format string, args ...interface{}) *Bottom {
	// TODO: consider renaming ot NewBottomf: this is now confusing as we also
	// have Newf.
	err := c.Newf(format, args...)
	return &Bottom{
		Src:  c.src,
		Err:  err,
		Code: EvalError,
		Node: c.vertex,
	}
}

// AddErrf records an error in OpContext. It returns errors collected so far.
func (c *OpContext) AddErrf(format string, args ...interface{}) *Bottom {
	return c.AddErr(c.Newf(format, args...))
}

type frame struct {
	env *Environment
	err *Bottom
	src ast.Node
	ci  CloseInfo
}

// PushState resets c as if it was a newly created context
// with the same configuration c was created with,
// returning a value which should be used to restore the current
// state by passing it to a matching call to [OpContext.PopState].
//
// If src is nil, c will still refer to the same source node.
func (c *OpContext) PushState(env *Environment, src ast.Node) (saved frame) {
	saved.env = c.e
	saved.err = c.errs
	saved.src = c.src
	saved.ci = c.ci

	c.errs = nil
	if src != nil {
		c.src = src
	}
	c.e = env

	return saved
}

func (c *OpContext) PopState(s frame) *Bottom {
	err := c.errs
	c.e = s.env
	c.errs = s.err
	c.src = s.src
	c.ci = s.ci
	return err
}

func (c *OpContext) pushConjunct(x Conjunct) (saved frame) {
	src := x.Expr().Source()

	saved.env = c.e
	saved.err = c.errs
	saved.src = c.src
	saved.ci = c.ci

	c.errs = nil
	if src != nil {
		c.src = src
	}
	c.e = x.Env
	c.ci = x.CloseInfo

	return saved
}

func (c *OpContext) popConjunct(s frame) *Bottom {
	return c.PopState(s)
}

// PushArc signals c that arc v is currently being processed for the purpose
// of error reporting. PopArc should be called with the returned value once
// processing of v is completed.
func (c *OpContext) PushArc(v *Vertex) (saved *Vertex) {
	c.vertex, saved = v, c.vertex
	return saved
}
func (c *OpContext) PushArcAndLabel(v *Vertex) (saved *Vertex) {

	c.vertex, saved = v, c.vertex
	c.altPath = append(c.altPath, v)
	return saved
}

// PopArc signals completion of processing the current arc.
func (c *OpContext) PopArc(saved *Vertex) {
	c.vertex = saved
}

func (c *OpContext) PopArcAndLabel(saved *Vertex) {
	c.vertex = saved
	c.altPath = c.altPath[:len(c.altPath)-1]
}

// Resolve finds a node in the tree.
//
// Should only be used to insert Conjuncts. TODO: perhaps only return Conjuncts
// and error.
func (c *OpContext) Resolve(x Conjunct, r Resolver) (v *Vertex, b *Bottom) {
	defer func() {
		x := recover()
		switch x.(type) {
		case nil:
		case *scheduler:
			b = c.NewErrf("unresolved value %s", r)
		default:
			panic(x)
		}
	}()
	return c.resolveState(x, r, Flags{
		status:    finalized,
		condition: allKnown,
		mode:      finalize,
	})
}

func (c *OpContext) resolveState(x Conjunct, r Resolver, state Flags) (*Vertex, *Bottom) {
	s := c.pushConjunct(x)

	arc := r.resolve(c, state)

	err := c.PopState(s)
	if err != nil {
		return nil, err
	}

	if arc.ChildErrors != nil && arc.ChildErrors.Code == StructuralCycleError {
		return nil, arc.ChildErrors
	}

	// Dereference any vertices that do not contribute to more knownledge about
	// the node.
	arc = arc.DerefNonRooted()

	return arc, err
}

// Lookup looks up r in env without further resolving the value.
func (c *OpContext) Lookup(env *Environment, r Resolver) (*Vertex, *Bottom) {
	s := c.PushState(env, r.Source())

	arc := r.resolve(c, Flags{
		status:    partial,
		condition: allKnown,
		mode:      ignore,
	})

	err := c.PopState(s)

	return arc, err
}

// Validate calls validates value for the given validator.
//
// TODO(errors): return boolean instead: only the caller has enough information
// to generate a proper error message.
func (c *OpContext) Validate(check Conjunct, value Value) *Bottom {
	// TODO: use a position stack to push both values.

	// TODO: move to PushConjunct. Note that PushConjunct also saves and
	// restores the error, which may change how errors are reported.
	// saved := c.PushConjunct(check)

	src := c.src
	ci := c.ci
	env := c.e
	c.src = check.Source()
	c.ci = check.CloseInfo
	c.e = check.Env

	err := check.x.(Validator).validate(c, value)

	c.src = src
	c.ci = ci
	c.e = env

	return err
}

// concrete returns the concrete value of x after evaluating it.
// msg is used to mention the context in which an error occurred, if any.
func (c *OpContext) concrete(env *Environment, x Expr, msg interface{}) (result Value, complete bool) {
	s := c.PushState(env, x.Source())

	state := Flags{
		status:    partial,
		condition: concreteKnown,
		mode:      yield,
	}
	w := c.evalState(x, state)
	_ = c.PopState(s)

	w, ok := c.getDefault(w)
	if !ok {
		return w, false
	}
	v := Unwrap(w)

	complete = w != nil
	if !IsConcrete(v) {
		complete = false
		b := c.NewErrf("non-concrete value %v in operand to %s", w, msg)
		b.Code = IncompleteError
		v = b
	}

	return v, complete
}

// getDefault resolves a disjunction to a single value. If there is no default
// value, or if there is more than one default value, it reports an "incomplete"
// error and return false. In all other cases it will return true, even if
// v is already an error. v may be nil, in which case it will also return nil.
func (c *OpContext) getDefault(v Value) (result Value, ok bool) {
	w, d := resolveDefault(v)
	if d != nil {
		c.addErrf(IncompleteError, c.pos(),
			"unresolved disjunction %v (type %s)", d, d.Kind())
		return nil, false
	}
	return w, true
}

// resolveDefault resolves a disjunction to a single value, following default
// values through nested disjunctions and vertices. If v does not resolve to a
// single value, it returns the unresolved disjunction instead. It implements
// both [OpContext.getDefault] and [unresolvedDisjunction], which must agree
// on this traversal.
func resolveDefault(v Value) (result Value, unresolved *Disjunction) {
	var d *Disjunction
	switch x := v.(type) {
	default:
		return v, nil

	case *Vertex:
		// TODO: return vertex if not disjunction.
		switch t := x.BaseValue.(type) {
		case *Disjunction:
			d = t

		case *Vertex:
			return resolveDefault(t)

		default:
			return x, nil
		}

	case *Disjunction:
		d = x
	}

	if d.NumDefaults != 1 {
		return nil, d
	}
	return resolveDefault(d.Values[0])
}

// Evaluate evaluates an expression within the given environment and indicates
// whether the result is complete. It will always return a non-nil result.
func (c *OpContext) Evaluate(env *Environment, x Expr) (result Value, complete bool) {
	s := c.PushState(env, x.Source())

	val := c.evalState(x, Flags{
		status:    partial,
		condition: concreteKnown,
		mode:      finalize,
	})

	complete = true

	if err, _ := val.(*Bottom); err != nil && err.IsIncomplete() {
		complete = false
	}
	if val == nil {
		complete = false
		// TODO ENSURE THIS DOESN"T HAPPEN>
		val = &Bottom{
			Code: IncompleteError,
			Err:  c.Newf("UNANTICIPATED ERROR"),
			Node: env.DerefVertex(c),
		}

	}

	_ = c.PopState(s)

	if !complete || val == nil {
		return val, false
	}

	return val, true
}

// IsValidExpr evaluates expression x and reports whether it denotes a valid
// value, using full finalization (as the != _|_ comparison did) so that
// sub-field errors are detected. It returns false if x evaluates to a bottom
// of any flavor, incomplete as well as permanent, or if a permanent error is
// reachable within it. A value that is fine but has merely incomplete
// sub-fields (an open struct, an unresolved disjunction) is valid. Errors
// produced during evaluation are discarded after checking.
//
// A cycle error in evaluating x is returned rather than committed as false.
func (c *OpContext) IsValidExpr(env *Environment, x Expr) (bool, *Bottom) {
	valid, _, b := c.isValidExpr(env, x, false)
	return valid, b
}

// RecheckValidExpr reports whether committed is still the answer of
// [OpContext.IsValidExpr] for x once the node owning env has finalized. It is
// meant for the stability post-check of isValid.
//
// Besides re-evaluating x, it re-derives a value that is incomplete from that
// value's conjuncts: such a value may have been finalized while a field it
// depends on was still absent, and reading it again would just return the
// stale error. If the re-derived value is no longer an error, the committed
// answer did not hold.
func (c *OpContext) RecheckValidExpr(env *Environment, x Expr, committed bool) bool {
	valid, stale, b := c.isValidExpr(env, x, true)
	return b == nil && !stale && valid == committed
}

func (c *OpContext) isValidExpr(env *Environment, x Expr, recheck bool) (valid, stale bool, err *Bottom) {
	s := c.PushState(env, x.Source())
	v := c.evalState(x, Flags{
		status:    finalized,
		condition: needTasksDone,
		mode:      finalize,
	})
	// Save errors accumulated during evaluation (e.g. ChildErrors from
	// sub-evaluations propagated via c.AddBottom in BinaryExpr/StructLit.evaluate).
	errsFromEval := c.errs
	// getDefault may add an IncompleteError for unresolved disjunctions.
	// That should not make isValid return false: an unresolved disjunction
	// is a valid (abstract) value, not an error. Restore after the call.
	u, _ := c.getDefault(v)
	c.errs = errsFromEval

	switch val := u.(type) {
	case *Bottom:
		switch val.Code {
		case CycleError:
			// A cycle in reading x, such as a mutual dependency between two
			// isValid conditions, is not an answer: propagate it as the
			// comparison against bottom and the other predicates do, rather
			// than committing a definite false that depends on the order in
			// which the cycle was broken.
			_ = c.PopState(s)
			return false, false, val

		case IncompleteError:
			if r, ok := x.(Resolver); ok && recheck {
				// x refers to a node whose value is the incomplete error, as
				// for isValid(v.e) where v.e refers to an absent field.
				if arc, _ := c.Resolve(MakeConjunct(env, x, c.ci), r); arc != nil {
					stale = c.isStaleIncomplete(arc)
				}
			}
		}

	case *Vertex:
		val.Finalize(c)

		// The expression is valid only if it resolved to a non-error value. A
		// bottom base value — incomplete as well as final — means it did not:
		// e.g. isValid(ref.missing & string), where unifying a missing reference
		// with a type leaves an incomplete bottom as the whole value. isFinalError
		// alone would miss the incomplete case and wrongly report valid. A value
		// that is itself fine but has merely incomplete *sub-fields* (an open
		// struct, an unresolved disjunction) keeps a non-bottom base value and so
		// stays valid, matching isValid(b) for b: {c?: {}, d: c}.
		// c.errs covers ChildErrors propagated by BinaryExpr/StructLit.evaluate
		// (inline struct/binary expressions are re-evaluated with c.inValidator>0).
		_, isErr := Unwrap(val.DerefValue()).(*Bottom)
		valid = !isErr && c.errs == nil
		if recheck && isErr {
			stale = c.isStaleIncomplete(val)
		}

	case *BuiltinValidator:
		// A self-less validator call such as countN(...) checks its own
		// arguments. Its call returns only permanent failures, leaving a
		// violation that may still be resolved to the validator. Run it to
		// read either: the expression is valid only if validation passes.
		valid = c.errs == nil
		if val.Builtin.ValidatorNoSelf && valid {
			valid = val.validate(c, nil) == nil
		}

	default:
		// nil means the value is abstract/incomplete but not an error.
		// Non-nil non-vertex/bottom values (scalars) are valid.
		valid = c.errs == nil
	}
	_ = c.PopState(s)
	return valid, stale, nil
}

// isStaleIncomplete reports whether the value of n is an incomplete error that
// no longer holds: re-deriving n from its conjuncts against the current
// configuration yields a non-error value. This happens when n was evaluated
// while a field it refers to was still absent, and that field was added later,
// for instance by a comprehension that depended on n being an error.
//
// Only n itself is re-derived. A stale error that n merely inherits from a
// node it refers to is not detected.
func (c *OpContext) isStaleIncomplete(n *Vertex) bool {
	return c.rederiveStale(n) != nil
}

// rederiveStale returns the value of n re-derived from its conjuncts if the
// value of n is an incomplete error that no longer holds, and nil otherwise.
// See [OpContext.isStaleIncomplete].
func (c *OpContext) rederiveStale(n *Vertex) *Vertex {
	b, _ := Unwrap(n.DerefValue()).(*Bottom)
	if b == nil || !b.IsIncomplete() {
		return nil
	}
	saved := c.errs
	defer func() { c.errs = saved }()

	fresh := &Vertex{Parent: n.Parent, Label: n.Label}
	for x := range n.LeafConjuncts() {
		fresh.InsertConjunct(x)
	}
	fresh.Finalize(c)
	if _, isErr := Unwrap(fresh.DerefValue()).(*Bottom); isErr {
		return nil
	}
	return fresh
}

// RederiveStaleExpr is meant for the stability post-check of a reflection
// builtin that committed an answer for x because x was incomplete. It reports
// the value that the node x refers to has once the node owning env has
// finalized, if the incomplete error no longer holds, and nil if it still
// does. Reading x again would just return the stale error; see
// [OpContext.RecheckValidExpr].
func (c *OpContext) RederiveStaleExpr(env *Environment, x Expr) *Vertex {
	r, ok := x.(Resolver)
	if !ok {
		return nil
	}
	s := c.PushState(env, x.Source())
	defer func() { _ = c.PopState(s) }()

	arc, _ := c.Resolve(MakeConjunct(env, x, c.ci), r)
	if arc == nil {
		return nil
	}
	return c.rederiveStale(arc)
}

// EvaluateKeepState does an evaluate, but leaves any errors and cycle info
// within the context.
func (c *OpContext) EvaluateKeepState(x Expr) (result Value) {
	src := c.src
	c.src = x.Source()

	result, ci := c.evalStateCI(x, Flags{
		status:    partial,
		condition: concreteKnown,
		mode:      finalize,
	})

	c.src = src
	c.ci = ci

	return result
}

// value evaluates expression v within the current environment. The result may
// be nil if the result is incomplete. value leaves errors untouched to that
// they can be collected by the caller.
func (c *OpContext) value(x Expr, state Flags) (result Value) {
	state.concrete = true
	v := c.evalState(x, state)

	v, _ = c.getDefault(v)
	v = Unwrap(v)
	return v
}

// valueOrDisjunction is like [OpContext.value], except that a value which
// holds a disjunction is returned as that [Disjunction], defaults and all,
// instead of being resolved to its default or reported as ambiguous. It
// evaluates the arguments of builtins marked [Builtin.PerDisjunct].
func (c *OpContext) valueOrDisjunction(x Expr, state Flags) Value {
	state.concrete = true
	return c.disjunctionOrDefault(c.evalState(x, state))
}

// closedArg evaluates an argument of a builtin marked [Builtin.Closes]. It
// is like [OpContext.valueOrDisjunction], except that a reference to a
// vertex whose evaluation is incomplete is returned as that vertex rather
// than as its error: the vertex's conjuncts are evaluated again where the
// result of the call is unified, as they are for a plain reference there.
func (c *OpContext) closedArg(x Expr, state Flags) Value {
	state.concrete = true
	_, isRef := x.(Resolver)
	state.keepIncomplete = isRef
	v := c.evalState(x, state)
	if isRef && isIncompleteVertex(v) {
		return v
	}
	return c.disjunctionOrDefault(v)
}

// disjunctionOrDefault returns the disjunction v holds, defaults and all,
// or otherwise its default, unwrapped.
func (c *OpContext) disjunctionOrDefault(v Value) Value {
	if d := disjunctionOf(v); d != nil {
		return d
	}
	v, _ = c.getDefault(v)
	return Unwrap(v)
}

// isIncompleteVertex reports whether v is a vertex whose evaluation is
// incomplete, such as a struct with a comprehension that reads a field
// which is not yet concrete.
func isIncompleteVertex(v Value) bool {
	x, ok := v.(*Vertex)
	if !ok {
		return false
	}
	b := x.Bottom()
	return b != nil && b.IsIncomplete()
}

// disjunctionOf returns the disjunction v holds, looking through vertices
// whose value is another vertex, or nil if v is not a disjunction.
func disjunctionOf(v Value) *Disjunction {
	if x, ok := v.(*Vertex); ok {
		d, _ := x.DerefValue().BaseValue.(*Disjunction)
		return d
	}
	d, _ := v.(*Disjunction)
	return d
}

func (c *OpContext) evalState(v Expr, state Flags) (result Value) {
	result, _ = c.evalStateCI(v, state)
	return result
}

func (c *OpContext) evalStateCI(v Expr, state Flags) (result Value, ci CloseInfo) {
	result, ci, _ = c.evalStateRef(v, state)
	return result, ci
}

// evalStateRef is like [OpContext.evalStateCI]. If v is a reference, it also
// returns the vertex that the reference resolved to, before it is
// dereferenced: what is recorded on a vertex that shares another, such as
// [Vertex.OpenedShared], is otherwise lost.
func (c *OpContext) evalStateRef(v Expr, state Flags) (result Value, ci CloseInfo, ref *Vertex) {
	savedSrc := c.src
	c.src = v.Source()
	err := c.errs
	c.errs = nil
	// Save the old CloseInfo and restore after evaluate to avoid detecting
	// spurious cycles.
	saved := c.ci

	defer func() {
		c.errs = CombineErrors(c.src, c.errs, err)

		if v, ok := result.(*Vertex); ok {
			if b := v.Bottom(); b != nil {
				switch b.Code {
				case IncompleteError:
				case CycleError:
					break
				default:
					result = b
				}
			}
		}

		// TODO: remove this when we handle errors more principally.
		if b, ok := result.(*Bottom); ok {
			result = c.wrapCycleError(c.src, b)
			if c.errs != result {
				c.errs = CombineErrors(c.src, c.errs, result)
			}
		}
		if c.errs != nil {
			result = c.errs
		}
		c.src = savedSrc

		// TODO(evalv3): this c.ci should be passed to the caller who may need
		// it to continue cycle detection for partially evaluated values.
		// Either this or we must prove that this is covered by structural cycle
		// detection.
		c.ci = saved
	}()

	switch x := v.(type) {
	case Value:
		return x, c.ci, ref

	case *OpenExpr:
		v, ci := c.evalStateCI(x.X, state)
		ci.Opened = true
		return v, ci, nil

	case Evaluator:
		v := x.evaluate(c, state)
		return v, c.ci, ref

	case Resolver:
		arc := x.resolve(c, state)
		if c.HasErr() {
			return nil, c.ci, ref
		}
		if arc == nil {
			return nil, c.ci, ref
		}
		orig := arc
		ref = arc
		// TODO(deref): what is the right level of dereferencing here?
		// DerefValue seems to work too.
		arc = arc.DerefNonShared()

		// TODO: is this indirect necessary?
		// arc = arc.Indirect()
		if s := arc.getState(c); s != nil {
			c.ci, _ = s.detectCycle(arc, nil, x, c.ci)

			defer s.retainProcess().releaseProcess()

			origNeeds := state.condition
			needs := origNeeds | arcTypeKnown
			runMode := state.mode

			switch runMode {
			case finalize:
				arc.unify(c, Flags{condition: needs, mode: attemptOnly, checkTypos: true}) // to set scalar
				s.freeze(needs, x)
			case attemptOnly:
				arc.unify(c, Flags{condition: needs, mode: attemptOnly, checkTypos: true}) // to set scalar

			case yield:
				arc.unify(c, Flags{condition: needs, mode: runMode, checkTypos: true}) // to set scalar

				evaluating := arc.status == evaluating
				if state.concrete && orig != arc && orig.state != nil && orig.state.meets(scalarKnown) && IsRecursivelyConcrete(arc) {
					evaluating = false
				}

				// We cannot resolve a value that represents an unresolved
				// disjunction.
				if evaluating && orig != arc && arc.IsDisjunct {
					task := c.current()
					if origNeeds == scalarKnown && !orig.state.meets(scalarKnown) {
						orig.state.defaultAttemptInCycle = task.node.node
						task.waitFor(&orig.state.scheduler, needs)
						s.yield()
						panic("unreachable")
					}
					err := c.Newf("unresolved disjunction: %v", x)
					b := &Bottom{Code: CycleError, Err: err}
					return b, c.ci, ref
				}

				hasCycleBreakingValue := s.hasFieldValue ||
					!isCyclePlaceholder(arc.BaseValue)

				// The arc's scheduler may have FROZEN scalarKnown
				// (meaning its value transition completed at the scheduler
				// level) yet arc.BaseValue is still the cycle sentinel.
				// That happens for the inline vertex created by
				// cross-package import unification of a definition whose
				// body has a comprehension reading a sibling field: the
				// scheduler is done with the arc but the BaseValue
				// transition did not propagate. A real cycle leaves
				// scalarKnown un-frozen because evaluation never settled,
				// so this is a safe distinguisher.
				if evaluating && !hasCycleBreakingValue &&
					s.frozen&scalarKnown != 0 && allTasksFinished(s) {
					arc.Finalize(c)
					hasCycleBreakingValue = !isCyclePlaceholder(arc.BaseValue)
				}

				if evaluating && !hasCycleBreakingValue {
					err := c.Newf("cycle with field: %v", x)
					b := &Bottom{Code: CycleError, Err: err}
					c.AddBottom(b)
					break
				}

				v := c.evaluate(arc, x, state)

				// A dynamic vertex itself is completed by whatever
				// created it.
				if arc.Internal() && !arc.IsDynamic && !allTasksFinished(s) {
					if n := c.rootedTaskNode(); n != nil {
						n.scheduleTask(handleDeferred, nil, arc, CloseInfo{})
					}
				}

				return v, c.ci, ref
			}
		}
		arc = arc.DerefValue()
		v := c.evaluate(arc, x, state)

		return v, c.ci, ref

	default:
		// This can only happen, really, if v == nil, which is not allowed.
		panic(fmt.Sprintf("unexpected Expr type %T", v))
	}
}

// wrapCycleError converts the sentinel cycleError in a concrete one with
// position information.
func (c *OpContext) wrapCycleError(src ast.Node, b *Bottom) *Bottom {
	if src != nil &&
		b.Code == CycleError &&
		len(errors.Positions(b.Err)) == 0 {
		bb := *b
		bb.Err = errors.Wrapf(b.Err, src.Pos(), "")
		b = &bb
	}
	return b
}

// unifyNode returns a possibly partially evaluated node value.
//
// TODO: maybe return *Vertex, *Bottom
func (c *OpContext) unifyNode(expr Expr, state Flags) (result Value) {
	result, _ = c.unifyNodeRef(expr, state)
	return result
}

// unifyNodeRef is like [OpContext.unifyNode]. If expr is a reference, it also
// returns the vertex that the reference resolved to, before it is
// dereferenced; see [OpContext.evalStateRef].
func (c *OpContext) unifyNodeRef(expr Expr, state Flags) (result Value, ref *Vertex) {
	savedSrc := c.src
	c.src = expr.Source()
	err := c.errs
	c.errs = nil

	defer func() {
		c.errs = CombineErrors(c.src, c.errs, err)

		if v, ok := result.(*Vertex); ok {
			if b := v.Bottom(); b != nil && !b.IsIncomplete() {
				// TODO: a child error only holds the error of the first
				// erroneous child of v. Report all of them, without
				// validating v again for every reference to it.
				// See erroneousFields in references/errors.txtar.
				result = b
			}
		}

		// TODO: remove this when we handle errors more principally.
		if b, ok := result.(*Bottom); ok {
			if c.src != nil &&
				b.Code == CycleError &&
				!b.Err.Position().IsValid() &&
				len(b.Err.InputPositions()) == 0 {
				bb := *b
				bb.Err = errors.Wrapf(b.Err, c.src.Pos(), "")
				result = &bb
			}
			c.errs = CombineErrors(c.src, c.errs, result)
		}
		if c.errs != nil {
			result = c.errs
		}
		c.src = savedSrc
	}()

	var v *Vertex

	switch x := expr.(type) {
	case Value:
		return x, ref

	case Evaluator:
		return x.evaluate(c, state), ref

	case Resolver:
		v = x.resolve(c, state)
		ref = v

	default:
		// This can only happen, really, if v == nil, which is not allowed.
		panic(fmt.Sprintf("unexpected Expr type %T", expr))
	}

	if c.HasErr() {
		return nil, ref
	}
	if v == nil {
		return nil, ref
	}
	// Processing v may make it share a vertex not processed yet, which the
	// lookup then reads in its place. Process that vertex too, rather than
	// unifying v to reach it: the lookup must count as non-cyclic structure
	// on the vertex it reads.
	v = v.DerefValue()
	for {
		c.processRef(v)
		w := v.DerefValue()
		if w == v {
			return v, ref
		}
		v = w
	}
}

// processRef evaluates v far enough for a reference to it to be resolved.
func (c *OpContext) processRef(v *Vertex) {
	n := v.getState(c)
	if n == nil {
		return
	}
	defer n.retainProcess().releaseProcess()

	// A lookup counts as new structure. See the comment in Section
	// "Lookups in inline cycles" in cycle.go.
	if !c.ci.IsCyclic() {
		n.hasNonCycle = true
	}

	// Always yield to not get spurious errors.
	n.process(arcTypeKnown|fieldSetKnown, yield)
	// It is possible that the node is only midway through
	// evaluating a disjunction. In this case, we want to ensure
	// that disjunctions are finalized, so that disjunction shows
	// up in BaseValue.
	if len(n.disjuncts) > 0 {
		n.node.unify(c, Flags{condition: arcTypeKnown | fieldSetKnown, mode: yield, checkTypos: false})
	}
}

func (c *OpContext) lookup(x *Vertex, pos token.Pos, l Feature, flags Flags) *Vertex {
	return x.lookup(c, pos, l, flags)
}

func (c *OpContext) undefinedFieldError(v *Vertex, code ErrorCode) {
	label := v.Label.SelectorString(c)
	c.addErrf(code, c.pos(), "undefined field: %s", label)
}

func (c *OpContext) Label(src Expr, x Value) Feature {
	return LabelFromValue(c, src, x)
}

func (c *OpContext) typeError(v Value, k Kind) {
	if isError(v) {
		return
	}
	if (!IsConcrete(v) && v.Kind()&k != 0) || c.hasUnresolvedAncestorCycle(v) {
		c.addErrf(IncompleteError, Pos(v), "incomplete %s: %s", k, v)
	} else {
		c.AddErrf("cannot use %s (type %s) as type %s", v, v.Kind(), k)
	}
}

func (c *OpContext) typeErrorAs(v Value, k Kind, as interface{}) {
	if as == nil {
		c.typeError(v, k)
		return
	}
	if isError(v) {
		return
	}
	if (!IsConcrete(v) && v.Kind()&k != 0) || c.hasUnresolvedAncestorCycle(v) {
		c.addErrf(IncompleteError, Pos(v),
			"incomplete %s in %v: %s", k, as, v)
	} else {
		c.AddErrf("cannot use %s (type %s) as type %s in %v", v, v.Kind(), k, as)
	}
}

// hasUnresolvedAncestorCycle reports whether v is a Vertex that only appears
// concrete because it is an under-resolved copy of an ancestor reached
// through a structural cycle, such as the vertex bound by `let X = self`;
// scalar conversion must then report an incomplete error, not a permanent
// one. See https://cuelang.org/issue/4420.
//
// TODO(cycle): this compensates for conjuncts parked by the CallExpr
// deferment in [nodeContext.scheduleConjunct]; better would be to not
// propagate the cyclic marking onto fresh inline vertices at all.
func (c *OpContext) hasUnresolvedAncestorCycle(v Value) bool {
	x, ok := v.(*Vertex)
	return ok && x.state != nil && x.state.ctx.opID == c.opID &&
		x.state.hasOnlyCyclicConjuncts()
}

var emptyNode = &Vertex{status: finalized}

// node is called by SelectorExpr.resolve and IndexExpr.resolve.
func (c *OpContext) node(orig Node, x Expr, scalar bool, state Flags) *Vertex {
	// Do not treat inline structs as closed by default if within a schema.
	// See comment at top of scheduleVertexConjuncts.
	if _, ok := x.(Resolver); !ok {
		saved := c.ci.FromDef
		c.ci.FromDef = false
		defer func() { c.ci.FromDef = saved }()
	}

	// TODO: always get the vertex. This allows a whole bunch of trickery
	// down the line.
	v := c.unifyNode(x, state)

	v, ok := c.getDefault(v)
	if !ok {
		// Error already generated by getDefault.
		return emptyNode
	}

	// The two if blocks below are rather subtle. If we have an error of
	// the sentinel value cycle, we have earlier determined that the cycle is
	// allowed and that it can be ignored here. Any other CycleError is an
	// annotated cycle error that could be taken as is.
	// TODO: do something simpler.
	if scalar {
		if w := Unwrap(v); !isCyclePlaceholder(w) {
			v = w
		}
	}

	node, ok := v.(*Vertex)
	if ok && !isCyclePlaceholder(node.BaseValue) {
		v = node.Value()
	}

	switch nv := v.(type) {
	case nil:
		c.addErrf(IncompleteError, Pos(x),
			"%s undefined (%s is incomplete)", orig, x)
		return emptyNode

	case *Bottom:
		// TODO: this is a bit messy. In some cases errors are already added
		// and in some cases not. Not a huge deal, as errors will be uniqued
		// down the line, but could be better.
		c.AddBottom(nv)
		return emptyNode

	case *Vertex:
		if node == nil {
			panic("unexpected markers with nil node")
		}
		// Needed for package dep: dep does partial evaluation of expressions
		// while traversing values. Not evaluating the node here could lead
		// to a lookup in an unevaluated node, resulting in erroneously failing
		// lookups.
		if nv.nonRooted {
			nv.CompleteArcsShallow(c)
		}
	default:
		if kind := v.Kind(); kind&StructKind != 0 {
			c.addErrf(IncompleteError, Pos(x),
				"%s undefined as %s is incomplete (type %s)", orig, x, kind)
			return emptyNode

		} else if !ok {
			c.addErrf(0, Pos(x), // TODO(error): better message.
				"invalid operand %s (found %s, want list or struct)",
				x.Source(), v.Kind())
			return emptyNode
		}
	}

	return node
}

// List returns the list value v with its elements evaluated.
//
// Iterate over the elements with [Vertex.Elems] rather than returning an
// iterator from here: only a method small enough to inline lets the compiler
// keep the loop's closures on the stack.
func (c *OpContext) List(v Value) *Vertex {
	list := c.RawList(v)
	list.Finalize(c)
	return list
}

// RawList is like [OpContext.List] but does not evaluate the elements.
func (c *OpContext) RawList(v Value) *Vertex {
	if v != nil {
		if a, ok := c.getDefault(v); ok {
			v = a
		}
	}
	x, ok := v.(*Vertex)
	if !ok || !x.IsList() {
		c.typeError(v, ListKind)
		return emptyNode
	}
	return x
}

var zero = &Num{K: NumberKind}

func (c *OpContext) Num(v Value, as interface{}) *Num {
	v = Unwrap(v)
	if isError(v) {
		return zero
	}
	x, ok := v.(*Num)
	if !ok {
		c.typeErrorAs(v, NumberKind, as)
		return zero
	}
	return x
}

func (c *OpContext) uint64(v Value, as string) uint64 {
	v = Unwrap(v)
	if isError(v) {
		return 0
	}
	x, ok := v.(*Num)
	if !ok || x.K != IntKind {
		c.typeErrorAs(v, IntKind, as)
		return 0
	}
	if x.X.Negative {
		// TODO: improve message
		c.AddErrf("cannot convert negative number to uint64")
		return 0
	}
	i, ok := x.Uint64()
	if !ok {
		// TODO: improve message
		c.AddErrf("cannot convert number %s to uint64", &x.X)
		return 0
	}
	return i
}

func (c *OpContext) BoolValue(v Value) bool {
	return c.boolValue(v, nil)
}

func (c *OpContext) boolValue(v Value, as interface{}) bool {
	v = Unwrap(v)
	if isError(v) {
		return false
	}
	x, ok := v.(*Bool)
	if !ok {
		c.typeErrorAs(v, BoolKind, as)
		return false
	}
	return x.B
}

func (c *OpContext) StringValue(v Value) string {
	return c.stringValue(v, nil)
}

// ToBytes returns the bytes value of a scalar value.
func (c *OpContext) ToBytes(v Value) []byte {
	if x, ok := v.(*Bytes); ok {
		return x.B
	}
	return []byte(c.ToString(v))
}

// ToString returns the string value of a scalar value.
func (c *OpContext) ToString(v Value) string {
	return c.toStringValue(v, StringKind|NumberKind|BytesKind|BoolKind, nil)

}

func (c *OpContext) stringValue(v Value, as interface{}) string {
	return c.toStringValue(v, StringKind, as)
}

func (c *OpContext) toStringValue(v Value, k Kind, as interface{}) string {
	v = Unwrap(v)
	if isError(v) {
		return ""
	}
	if v.Kind()&k == 0 {
		if as == nil {
			c.typeError(v, k)
		} else {
			c.typeErrorAs(v, k, as)
		}
		return ""
	}
	switch x := v.(type) {
	case *String:
		return x.Str

	case *Bytes:
		return bytesToString(x.B)

	case *Num:
		return x.X.String()

	case *Bool:
		if x.B {
			return "true"
		}
		return "false"

	default:
		c.addErrf(IncompleteError, c.pos(),
			"non-concrete value %s (type %s)", v, v.Kind())
	}
	return ""
}

func bytesToString(b []byte) string {
	return strings.ToValidUTF8(string(b), string(utf8.RuneError))
}

func (c *OpContext) bytesValue(v Value, as interface{}) []byte {
	v = Unwrap(v)
	if isError(v) {
		return nil
	}
	x, ok := v.(*Bytes)
	if !ok {
		c.typeErrorAs(v, BytesKind, as)
		return nil
	}
	return x.B
}

var matchNone = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("^$")
})

// regexpCache caches compiled regular expressions by pattern string.
// Uses weak references so unused patterns can be garbage collected.
var regexpCache = newMemoizer(func(pattern string) (*regexp.Regexp, error) {
	// TODO(mvdan): consider simplifying patterns (which regexp/syntax can do)
	// before we look up or insert on the weak map? so that e.g. fo[o] bar{1} and
	// foo bar share the same entry.
	return regexp.Compile(pattern)
})

// CompileRegexp is like [regexp.Compile], but reuses compiled patterns
// while they are still referenced. Failures are not cached.
// The result is shared, so [regexp.Regexp.Longest] must not be called on it.
func CompileRegexp(pattern string) (*regexp.Regexp, error) {
	return regexpCache.get(pattern)
}

func (c *OpContext) regexp(v Value) *regexp.Regexp {
	v = Unwrap(v)
	if isError(v) {
		return matchNone()
	}
	var pattern string
	switch x := v.(type) {
	case *String:
		pattern = x.Str
	case *Bytes:
		pattern = string(x.B)
	default:
		c.typeError(v, StringKind|BytesKind)
		return matchNone()
	}
	re, err := regexpCache.get(pattern)
	if err != nil {
		c.AddErrf("invalid regexp: %s", err)
		return matchNone()
	}
	return re
}

func (c *OpContext) NewInt64(n int64, sources ...Node) Value {
	if c.HasErr() {
		return c.Err()
	}
	d := apd.New(n, 0)
	return &Num{Src: c.src, X: *d, K: IntKind}
}

func (c *OpContext) NewString(s string) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &String{Src: c.src, Str: s}
}

func (c *OpContext) newBytes(b []byte) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &Bytes{Src: c.src, B: b}
}

var (
	StaticBoolFalse = &Bool{B: false}
	StaticBoolTrue  = &Bool{B: true}
)

func (c *OpContext) NewBool(b bool) Value {
	if c.HasErr() {
		return c.Err()
	}
	// Creating boolean values is a very common operation,
	// such as when evaluating unary and binary operators.
	// A significant portion of the time, no source is attached
	// to the operation, so we can reuse Bool allocations.
	if c.src == nil {
		if b {
			return StaticBoolTrue
		} else {
			return StaticBoolFalse
		}
	}
	return &Bool{Src: c.src, B: b}
}

// String reports a string of x, for use in errors or debugging.
// Use [OpContext.Str] instead for %s format arguments, as it delays the work.
func (c *OpContext) String(x Node) string {
	return c.FormatNode(x)
}

// Str reports a string of x via a [fmt.Stringer], for use in errors or debugging.
func (c *OpContext) Str(x Node) fmt.Stringer {
	return Formatter{X: x, R: c.Runtime}
}

// Formatter wraps an adt.Node with the necessary information to print it.
//
// TODO: we could eliminate the need for this by ensuring that errors are
// _always_ formatted with a printer. We are not far off from this goal, but
// we need to verify several things.
// This is mainly possible because we intend to have a global string index
// using weak references. It also assumes that errors are always printed
// equally.
type Formatter struct {
	X Node

	// TODO: is runtime needed? Probably not if we have a global string index.
	R Runtime
}

func (f Formatter) String() string { return f.R.FormatNode(f.X) }

// NewList returns a new list for the given values.
func (c *OpContext) NewList(values ...Value) *Vertex {
	// TODO: consider making this a literal list instead.
	list := &ListLit{}
	v := c.newInlineVertex(nil, nil, Conjunct{Env: nil, x: list})

	for _, x := range values {
		list.Elems = append(list.Elems, x)
	}
	return v
}
