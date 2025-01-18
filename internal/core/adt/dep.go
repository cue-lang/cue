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
