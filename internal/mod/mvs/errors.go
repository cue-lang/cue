// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mvs

import (
	"fmt"
	"strings"
)

// BuildListError decorates an error that occurred gathering requirements
// while constructing a build list. BuildListError prints the chain
// of requirements to the module where the error occurred.
type BuildListError[V comparable] struct {
	Err   error
	stack []buildListErrorElem[V]
	vs    Versions[V]
}

type buildListErrorElem[V comparable] struct {
	m V

	// nextReason is the reason this module depends on the next module in the
	// stack. Typically either "requires", or "updating to".
	nextReason string
}

// NewBuildListError returns a new BuildListError wrapping an error that
// occurred at a module found along the given path of requirements and/or
// upgrades, which must be non-empty.
//
// The isVersionChange function reports whether a path step is due to an
// explicit upgrade or downgrade (as opposed to an existing requirement in a
// go.mod file). A nil isVersionChange function indicates that none of the path
// steps are due to explicit version changes.
func NewBuildListError[V comparable](err error, path []V, vs Versions[V], isVersionChange func(from, to V) bool) *BuildListError[V] {
	stack := make([]buildListErrorElem[V], 0, len(path))
	for len(path) > 1 {
		reason := "requires"
		if isVersionChange != nil && isVersionChange(path[0], path[1]) {
			reason = "updating to"
		}
		stack = append(stack, buildListErrorElem[V]{
			m:          path[0],
			nextReason: reason,
		})
		path = path[1:]
	}
	stack = append(stack, buildListErrorElem[V]{m: path[0]})

	return &BuildListError[V]{
		Err:   err,
		stack: stack,
		vs:    vs,
	}
}

// Module returns the module where the error occurred. If the module stack
// is empty, this returns a zero value.
func (e *BuildListError[V]) Module() V {
	if len(e.stack) == 0 {
		return *new(V)
	}
	return e.stack[len(e.stack)-1].m
}

func (e *BuildListError[V]) Error() string {
	b := &strings.Builder{}
	stack := e.stack

	// Don't print modules at the beginning of the chain without a
	// version. These always seem to be the main module or a
	// synthetic module ("target@").
	for len(stack) > 0 && e.vs.Version(stack[0].m) == "" {
		stack = stack[1:]
	}

	if len(stack) == 0 {
		b.WriteString(e.Err.Error())
	} else {
		for _, elem := range stack[:len(stack)-1] {
			fmt.Fprintf(b, "%v %s\n\t", elem.m, elem.nextReason)
		}
		m := stack[len(stack)-1].m
		fmt.Fprintf(b, "%v: %v", m, e.Err)
		// TODO the original mvs code was careful to ensure that the final module path
		// and version were included as part of the error message, but it did that
		// by checking for mod/module-specific error types, but we don't want this
		// package to depend on module. We could potentially do it by making those
		// errors implement interface types defined in this package.
	}
	return b.String()
}
