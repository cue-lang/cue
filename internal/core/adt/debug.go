// Copyright 2023 CUE Authors
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
	"log"
)

// depKind is a type of dependency that is tracked with incDependent and
// decDependent. For each there should be matching pairs passed to these
// functions. The debugger, when used, tracks and verifies that these
// dependencies are balanced.
type depKind int

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

	// EVAL tracks that the conjunct associated with a closeContext has been
	// inserted using scheduleConjunct. A closeContext may not be deleted
	// as long as the conjunct has not been evaluated yet.
	// This prevents a node from being released if an ARC decrement happens
	// before a node is evaluated.
	EVAL

	// ROOT dependencies are used to track that all nodes of parents are
	// added to a tree.
	ROOT // Always refers to self.

	INIT // nil, like defer

	// DEFER is used to track recursive processing of a node.
	DEFER // Always refers to self.
	// TEST is used for testing notifications.
	TEST // Always refers to self.
	SPAWN
)

func (k depKind) String() string {
	switch k {
	case PARENT:
		return "PARENT"
	case ARC:
		return "ARC"
	case NOTIFY:
		return "NOTIFY"
	case TASK:
		return "TASK"
	case EVAL:
		return "EVAL"
	case ROOT:
		return "ROOT"

	case INIT:
		return "INIT"
	case DEFER:
		return "DEFER"
	case TEST:
		return "TEST"
	case SPAWN:
		return "SPAWN"
	}
	panic("unreachable")
}

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

// DebugDeps enables dependency tracking for debugging purposes.
// It is off by default, as it adds a significant overhead.
var DebugDeps = false

func (c *closeContext) addDependent(kind depKind, dependant *closeContext) *ccDep {
	if !DebugDeps {
		return nil
	}

	if dependant == nil {
		dependant = c
	}

	if Verbosity > 1 {
		var state *nodeContext
		if c.src != nil && c.src.state != nil {
			state = c.src.state
		} else if dependant != nil && dependant.src != nil && dependant.src.state != nil {
			state = dependant.src.state
		}
		if state != nil {
			state.Logf("INC(%s, %d) %v; %p (parent: %p) <= %p\n", kind, c.conjunctCount, c.Label(), c, c.parent, dependant)
		} else {
			log.Printf("INC(%s) %v %p parent: %p %d\n", kind, c.Label(), c, c.parent, c.conjunctCount)
		}
	}

	dep := &ccDep{kind: kind, dependency: dependant}
	c.dependencies = append(c.dependencies, dep)

	return dep
}

// matchDecrement checks that this decrement matches a previous increment.
func (c *closeContext) matchDecrement(v *Vertex, kind depKind, dependant *closeContext) {
	if !DebugDeps {
		return
	}

	if dependant == nil {
		dependant = c
	}

	if Verbosity > 1 {
		if v.state != nil {
			v.state.Logf("DEC(%s) %v %p %d\n", kind, c.Label(), c, c.conjunctCount)
		} else {
			log.Printf("DEC(%s) %v %p %d\n", kind, c.Label(), c, c.conjunctCount)
		}
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

	panic("unmatched decrement")
}
