// Copyright 2022 CUE Authors
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

// Package stats is an experimental package for getting statistics on CUE
// evaluations.
package stats

import (
	"strings"
	"sync"
	"text/template"

	"cuelang.org/go/internal"
)

// Counts holds counters for key events during a CUE evaluation.
//
// This is an experimental type and the contents may change without notice.
type Counts struct {
	// Note that we can't use the public [cuecontext.EvalVersion] type
	// as that would lead to an import cycle. We could use "int" but that's a bit odd.
	// There's no harm in referencing an internal type in practice, given that
	// the public type is a type alias for the internal type already.

	// EvalVersion is the evaluator version which was used for the CUE evaluation,
	// corresponding to one of the values under [cuelang.org/go/cue/cuecontext.EvalVersion].
	EvalVersion internal.EvaluatorVersion

	// Operation counters
	//
	// These counters account for several key operations.

	// Unifications counts the number of calls to adt.Unify
	Unifications int64

	// Disjuncts indicates the number of total disjuncts processed as part
	// of a Unify operation. A unification with no | operator counts as a
	// single disjunct, so Disjuncts is always greater than or equal to the
	// number of Unifications.
	//
	// If Disjuncts is much larger than Unification, this may indicate room
	// for optimization. In particular, most practical uses of disjunctions
	// should allow for near-linear processing.
	Disjuncts int64

	// Notifications counts how often a Vertex is added to the notification
	// queue. This is typically only the case when a Vertex is involved in
	// some kind of cycle, so this should be relatively low in practice
	// compared to the number of unifications.
	Notifications int64 // Number of notifications sent to nodes.

	// Conjuncts is an estimate of the number of conjunctions processed during
	// the calls to Unify. This includes the conjuncts added in the compilation
	// phase as well as the derivative conjuncts inserted from other nodes
	// after following references.
	//
	// A number of Conjuncts much larger than Disjuncts may indicate non-linear
	// algorithmic behavior.
	Conjuncts int64

	// Typo checking counters
	NumCloseIDs      int64 // Number of close IDs used
	ConjunctInfos    int64 // Number of conjunct infos created
	MaxConjunctInfos int64 // Maximum number of conjunct infos in a node
	MaxReqSets       int64 // Maximum number of requirement sets
	MaxRedirect      int64 // Maximum number of redirects in containsDefID

	// Exception counters
	//
	// These counters track exceptional conditions that occur during evaluation.

	// GenerationMismatch indicates the number of times a node was unified
	// with a different generation than the one it was created in.
	GenerationMismatch int64 // Number of exceptional unification cases

	// MisalignedConjunct indicates the number of conjuncts that were dropped
	// because they were not aligned with the current generation of the context.
	// Generally this happens because a previously finalized vertex is unified
	// in as a value, not constraint, in which case it is okay to ignore
	// closedness info. If it were included as a schema, top-level conjuncts
	// would be unified and mapped to a local tree.
	MisalignedConjunct int64

	// MisalignedConstraint indicates the number of constraints that were not
	// aligned. This is more likely to be a bug.
	MisalignedConstraint int64

	// SkippedNotification indicates the number of notifications that were
	// skipped because the value was already finalized. This may miss conjuncts
	// when it occurs during evaluation, but it may also be triggered during
	// dependency analysis, in which case it is benign.
	SkippedNotification int64

	// Dependency resolution counters

	// ResolveDep counts the number calls to markResolver in dep.go.
	ResolveDep int64

	// Buffer counters
	//
	// Each unification and disjunct operation is associated with an object
	// with temporary buffers. Reuse of this buffer is critical for performance.
	// The following counters track this.

	Freed    int64 // Number of buffers returned to the free pool.
	Reused   int64 // Number of times a buffer is reused instead of allocated.
	Allocs   int64 // Total number of allocated buffer objects.
	Retained int64 // Number of times a buffer is retained upon finalization.
}

// TODO: None of the methods below protect against overflows or underflows.
// If those start happening in practice, or if the counters get large enough,
// add checks on each of the operations.

func (c *Counts) Add(other Counts) {
	switch v, vo := c.EvalVersion, other.EvalVersion; {
	case v == internal.EvalVersionUnset:
		// The first time we add evaluator counts, we record the evaluator version being used.
		if vo == internal.EvalVersionUnset {
			panic("the first call to Counts.Add must provide an evaluator version")
		}
		c.EvalVersion = vo
	case v != vo:
		// Any further evaluator counts being added must match the same evaluator version.
		//
		// TODO(mvdan): this is currently not possible to enforce, as we collect stats globally
		// via [adt.AddStats] which includes stats from contexts created with different versions.
		// We likely need to refactor the collection of stats so that it is not global first.

		// panic(fmt.Sprintf("cannot mix evaluator versions in Counts.Add: %v vs %v", v, vo))
	}
	c.Unifications += other.Unifications
	c.Conjuncts += other.Conjuncts
	c.Disjuncts += other.Disjuncts
	c.Notifications += other.Notifications

	c.GenerationMismatch += other.GenerationMismatch
	c.MisalignedConjunct += other.MisalignedConjunct
	c.MisalignedConstraint += other.MisalignedConstraint
	c.SkippedNotification += other.SkippedNotification

	c.NumCloseIDs += other.NumCloseIDs
	c.ConjunctInfos += other.ConjunctInfos
	if other.MaxConjunctInfos > c.MaxConjunctInfos {
		c.MaxConjunctInfos = other.MaxConjunctInfos
	}
	if other.MaxReqSets > c.MaxReqSets {
		c.MaxReqSets = other.MaxReqSets
	}
	if other.MaxRedirect > c.MaxRedirect {
		c.MaxRedirect = other.MaxRedirect
	}
	c.ResolveDep += other.ResolveDep

	c.Freed += other.Freed
	c.Retained += other.Retained
	c.Reused += other.Reused
	c.Allocs += other.Allocs
}

func (c Counts) Since(start Counts) Counts {
	c.Unifications -= start.Unifications
	c.Conjuncts -= start.Conjuncts
	c.Disjuncts -= start.Disjuncts
	c.Notifications -= start.Notifications
	c.GenerationMismatch -= start.GenerationMismatch
	c.MisalignedConjunct -= start.MisalignedConjunct
	c.MisalignedConstraint -= start.MisalignedConstraint
	c.SkippedNotification -= start.SkippedNotification
	c.NumCloseIDs -= start.NumCloseIDs
	c.ConjunctInfos -= start.ConjunctInfos
	c.ResolveDep -= start.ResolveDep

	// For max values, we don't subtract since they represent peaks
	// c.MaxConjunctInfos and c.MaxReqSets and c.MaxRedirect remain as-is

	c.Freed -= start.Freed
	c.Retained -= start.Retained
	c.Reused -= start.Reused
	c.Allocs -= start.Allocs

	return c
}

// Leaks reports the number of nodeContext structs leaked. These are typically
// benign, as they will just be garbage collected, as long as the pointer from
// the original nodes has been eliminated or the original nodes are also not
// referred to. But Leaks may have notable impact on performance, and thus
// should be avoided.
func (s Counts) Leaks() int64 {
	return s.Allocs + s.Reused - s.Freed
}

var stats = sync.OnceValue(func() *template.Template {
	return template.Must(template.New("stats").Parse(`{{"" -}}

Leaks:  {{.Leaks}}
Freed:  {{.Freed}}
Reused: {{.Reused}}
Allocs: {{.Allocs}}
Retain: {{.Retained}}

Unifications: {{.Unifications}}
Conjuncts:    {{.Conjuncts}}
Disjuncts:    {{.Disjuncts}}{{if .Notifications}}
Notifications: {{.Notifications}}{{end}}{{if .ResolveDep}}
ResolveDep:   {{.ResolveDep}}{{end}}{{if or .GenerationMismatch .MisalignedConjunct .MisalignedConstraint .SkippedNotification}}
{{if .GenerationMismatch}}
GenerationMismatch: {{.GenerationMismatch}}{{end}}{{if .MisalignedConjunct}}
MisalignedConjunct: {{.MisalignedConjunct}}{{end}}{{if .MisalignedConstraint}}
MisalignedConstraint: {{.MisalignedConstraint}}{{end}}{{if .SkippedNotification}}
SkippedNotification: {{.SkippedNotification}}{{end}}{{end}}{{if .NumCloseIDs}}

NumCloseIDs: {{.NumCloseIDs}}{{end}}{{if or (ge .MaxReqSets 150) (ge .MaxConjunctInfos 8) (ge .MaxRedirect 2)}}

ConjunctInfos:       {{.ConjunctInfos}}
MaxConjunctInfos:    {{.MaxConjunctInfos}}{{if .MaxReqSets}}
MaxReqSets:          {{.MaxReqSets}}{{end}}{{if .MaxRedirect}}
MaxRedirect:         {{.MaxRedirect}}{{end}}{{end}}`))
})

func (s Counts) String() string {
	buf := &strings.Builder{}
	err := stats().Execute(buf, s)
	if err != nil {
		panic(err)
	}
	return buf.String()
}
