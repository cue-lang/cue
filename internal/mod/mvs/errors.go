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
	stack []V
	vs    Versions[V]
}

// NewBuildListError returns a new BuildListError wrapping an error that
// occurred at a module found along the given path of requirements,
// which must be non-empty.
func NewBuildListError[V comparable](err error, path []V, vs Versions[V]) *BuildListError[V] {
	return &BuildListError[V]{
		Err:   err,
		stack: path,
		vs:    vs,
	}
}

func (e *BuildListError[V]) Error() string {
	b := &strings.Builder{}
	stack := e.stack

	// Don't print modules at the beginning of the chain without a
	// version. These always seem to be the main module or a
	// synthetic module ("target@").
	for len(stack) > 0 && e.vs.Version(stack[0]) == "" {
		stack = stack[1:]
	}

	if len(stack) == 0 {
		b.WriteString(e.Err.Error())
	} else {
		for _, m := range stack[:len(stack)-1] {
			fmt.Fprintf(b, "%v requires\n\t", m)
		}
		m := stack[len(stack)-1]
		fmt.Fprintf(b, "%v: %v", m, e.Err)
		// TODO the original mvs code was careful to ensure that the final module path
		// and version were included as part of the error message, but it did that
		// by checking for mod/module-specific error types, but we don't want this
		// package to depend on module. We could potentially do it by making those
		// errors implement interface types defined in this package.
	}
	return b.String()
}
