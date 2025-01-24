// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt

import "fmt"

// depKind is a type of dependency that is tracked with incDependent and
// decDependent. For each there should be matching pairs passed to these
// functions. The debugger, when used, tracks and verifies that these
// dependencies are balanced.
type depKind int

//go:generate go run golang.org/x/tools/cmd/stringer -type=depKind

const (
	// PARENT dependencies are used to track the completion of parent
	// closedContexts within the closedness tree.
	PARENT depKind = iota + 1

	// ARC dependencies are used to track the completion of corresponding
	// closedContexts in parent Vertices.
	ARC

	// NOTIFY dependencies keep a note while dependent conjuncts are collected
	NOTIFY // root node of source

	// TASK dependencies are used to track the completion of a task.
	TASK

	// DISJUNCT is used to mark an incomplete disjunct.
	DISJUNCT

	// EVAL tracks that the conjunct associated with a closeContext has been
	// inserted using scheduleConjunct. A closeContext may not be deleted
	// as long as the conjunct has not been evaluated yet.
	// This prevents a node from being released if an ARC decrement happens
	// before a node is evaluated.
	EVAL

	// COMP tracks pending arcs in comprehensions.
	COMP

	// ROOT dependencies are used to track that all nodes of parents are
	// added to a tree.
	ROOT // Always refers to self.

	// INIT dependencies are used to hold ownership of a closeContext during
	// initialization and prevent it from being finalized when scheduling a
	// node's conjuncts.
	INIT

	// DEFER is used to track recursive processing of a node.
	DEFER // Always refers to self.

	// SHARED is used to track shared nodes. The processing of shared nodes may
	// change until all other conjuncts have been processed.
	SHARED

	// TEST is used for testing notifications.
	TEST // Always refers to self.
)

// ccDep is used to record counters which is used for debugging only.
// It is purpose is to be precise about matching inc/dec as well as to be able
// to traverse dependency.
type ccDep struct {
	dependency  *closeContext
	kind        depKind
	decremented bool

	// task keeps a reference to a task for TASK dependencies.
	task *task
	// taskID indicates the sequence number of a task within a scheduler.
	taskID int
}

func (c *closeContext) addDependent(ctx *OpContext, kind depKind, dependant *closeContext) *ccDep {
	if dependant == nil {
		dependant = c
	}

	if ctx.LogEval > 1 {
		ctx.Logf(ctx.vertex, "INC(%s) %v %p parent: %p %d\n", kind, c.Label(), c, c.parent, c.conjunctCount)
	}

	dep := &ccDep{kind: kind, dependency: dependant}
	c.dependencies = append(c.dependencies, dep)

	return dep
}

// matchDecrement checks that this decrement matches a previous increment.
func (c *closeContext) matchDecrement(ctx *OpContext, v *Vertex, kind depKind, dependant *closeContext) {
	if dependant == nil {
		dependant = c
	}

	if ctx.LogEval > 1 {
		ctx.Logf(ctx.vertex, "DEC(%s) %v %p %d\n", kind, c.Label(), c, c.conjunctCount)
	}

	for _, d := range c.dependencies {
		if d.kind != kind {
			continue
		}
		if d.dependency != dependant {
			continue
		}
		// Only one typ-dependant pair possible.
		if d.decremented {
			// There might be a duplicate entry, so continue searching.
			continue
		}

		d.decremented = true
		return
	}

	if DebugDeps {
		panic(fmt.Sprintf("unmatched decrement: %s", kind))
	}
}

// A ccDepRef x refers to the x.src.[arcs|notify][x.index]
//
// We use this instead of pointers, because the address may change when
// growing a slice. We use this instead mechanism instead of a pointers so
// that we do not need to maintain separate free buffers once we use pools of
// closeContext.
type ccDepRef struct {
	src   *closeContext
	kind  depKind
	index int
}

// addArc adds a dependent arc to c. If child is an arc, child.src == key
func (c *closeContext) addArcDependency(ctx *OpContext, matched bool, child *closeContext) {
	root := child.src.cc()

	// NOTE: do not increment
	// - either root closeContext or otherwise resulting from sub closeContext
	//   all conjuncts will be added now, notified, or scheduled as task.
	for _, a := range c.arcs {
		if a.root == root {
			panic("addArc: Label already exists")
		}
	}
	child.incDependent(ctx, ARC, c) // matched in decDependent REF(arcs)

	c.arcs = append(c.arcs, ccArc{
		matched: matched,
		root:    root,
		dst:     child,
	})

	root.externalDeps = append(root.externalDeps, ccDepRef{
		src:   c,
		kind:  ARC,
		index: len(c.arcs) - 1,
	})
}

func (c *closeContext) addNotifyDependency(ctx *OpContext, dst *closeContext) bool {
	for _, a := range c.notify {
		if a.dst == dst {
			return false
		}
	}
	dst.incDependent(ctx, NOTIFY, c) // matched in decDependent REF(arcs)

	c.notify = append(c.notify, ccNotify{dst: dst})

	root := dst.src.cc()
	root.externalDeps = append(root.externalDeps, ccDepRef{
		src:   c,
		kind:  NOTIFY,
		index: len(c.notify) - 1,
	})
	return true
}

// incDisjunct increases disjunction-related counters. We require kind to be
// passed explicitly so that we can easily find the points where certain kinds
// are used.
func (c *closeContext) incDisjunct(ctx *OpContext, kind depKind) {
	if kind != DISJUNCT {
		panic("unexpected kind")
	}
	c.incDependent(ctx, DISJUNCT, nil)

	// TODO: the counters are only used in debug mode and we could skip this
	// if debug is disabled.
	for ; c != nil; c = c.parent {
		c.disjunctCount++
	}
}

// decDisjunct decreases disjunction-related counters. We require kind to be
// passed explicitly so that we can easily find the points where certain kinds
// are used.
func (c *closeContext) decDisjunct(ctx *OpContext, kind depKind) {
	if kind != DISJUNCT {
		panic("unexpected kind")
	}
	c.decDependent(ctx, DISJUNCT, nil)

	// TODO: the counters are only used in debug mode and we could skip this
	// if debug is disabled.
	for ; c != nil; c = c.parent {
		c.disjunctCount--
	}
}

// incDependent needs to be called for any conjunct or child closeContext
// scheduled for c that is queued for later processing and not scheduled
// immediately.
func (c *closeContext) incDependent(ctx *OpContext, kind depKind, dependant *closeContext) (debug *ccDep) {
	if c.src == nil {
		panic("incDependent: unexpected nil src")
	}
	if dependant != nil && c.generation != dependant.generation {
		// TODO: enable this check.

		// panic(fmt.Sprintf("incDependent: inconsistent generation: %d %d", c.generation, dependant.generation))
	}
	debug = c.addDependent(ctx, kind, dependant)

	if c.done {
		openDebugGraph(ctx, c, "incDependent: already checked")

		panic(fmt.Sprintf("incDependent: already closed: %p", c))
	}

	c.conjunctCount++
	return debug
}

// decDependent needs to be called for any conjunct or child closeContext for
// which a corresponding incDependent was called after it has been successfully
// processed.
func (c *closeContext) decDependent(ctx *OpContext, kind depKind, dependant *closeContext) {
	v := c.src

	c.matchDecrement(ctx, v, kind, dependant)
	c.decDependentNoMatch(ctx, kind, dependant)
}

// decDependentNoMatch is like decDependent, but does not check for a matching
// increment. This is useful when a decrement is triggered during creating
// a disjunct overlay, as it obviates the need to create the matching debug
// dependency.
func (c *closeContext) decDependentNoMatch(ctx *OpContext, kind depKind, dependant *closeContext) {
	if c.conjunctCount == 0 {
		panic(fmt.Sprintf("negative reference counter %d %p", c.conjunctCount, c))
	}

	c.conjunctCount--
	if c.conjunctCount > 0 {
		return
	}

	c.done = true

	for i, a := range c.arcs {
		cc := a.dst
		if a.decremented {
			continue
		}
		c.arcs[i].decremented = true
		cc.decDependent(ctx, ARC, c)
	}

	for i, a := range c.notify {
		cc := a.dst
		if a.decremented {
			continue
		}
		c.notify[i].decremented = true
		cc.decDependent(ctx, NOTIFY, c)
	}

	if !c.updateClosedInfo(ctx) {
		return
	}

	p := c.parent

	p.decDependent(ctx, PARENT, c) // REF(decrement: spawn)

	// If we have started decrementing a child closeContext, the parent started
	// as well. If it is still marked as needing an EVAL decrement, which can
	// happen if processing started before the node was added, it is safe to
	// decrement it now. In this case the NOTIFY and ARC dependencies will keep
	// the nodes alive until they can be completed.
	if dep := p.needsCloseInSchedule; dep != nil {
		p.needsCloseInSchedule = nil
		p.decDependent(ctx, EVAL, dep)
	}
}

// breakIncomingNotifications walks over incoming arcs and forces any remaining
// work to be done.
func (n *nodeContext) breakIncomingNotifications(mode runMode) {
	v := n.node
	// TODO: replace with something more principled that does not piggyback on
	// debug information.
	for _, r := range v.cc().externalDeps {
		if r.kind != NOTIFY {
			continue
		}
		src := r.src
		a := &src.notify[r.index]
		if a.decremented {
			continue
		}
		if n := src.src.getState(n.ctx); n != nil {
			n.completeNodeTasks(mode)
		}
	}
}

// breakIncomingDeps breaks all incoming dependencies, which includes arcs and
// pending notifications and attempts all remaining work.
//
// We should only break incoming dependencies if we are finalizing nodes, as
// breaking them earlier can cause a "already closed" panic. To make sure of
// this, we force the caller to pass mode.
func (n *nodeContext) breakIncomingDeps(mode runMode) {
	if mode != finalize {
		return
	}

	// TODO: remove this block in favor of finalizing notification nodes,
	// or what have you. We have patched this to skip evaluating when using
	// disjunctions, but this is overall a brittle approach.
	for _, r := range n.node.cc().externalDeps {
		src := r.src
		switch r.kind {
		case ARC:
			a := &src.arcs[r.index]
			if a.decremented {
				continue
			}
			a.decremented = true

			src.src.unify(n.ctx, needTasksDone, attemptOnly)
			a.dst.decDependent(n.ctx, ARC, src)
		case NOTIFY:
			a := &src.notify[r.index]
			if a.decremented {
				continue
			}
			a.decremented = true

			src.src.unify(n.ctx, needTasksDone, attemptOnly)
			a.dst.decDependent(n.ctx, NOTIFY, src)
		}
	}
}
