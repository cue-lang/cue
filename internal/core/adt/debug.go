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
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/pborman/indent"
)

// RecordDebugGraph records debug output in ctx if there was an anomaly
// discovered.
func RecordDebugGraph(ctx *OpContext, v *Vertex, name string) {
	graph, hasError := CreateMermaidGraph(ctx, v, true)
	if hasError {
		if ctx.ErrorGraphs == nil {
			ctx.ErrorGraphs = map[string]string{}
		}
		path := ctx.PathToString(v.Path())
		ctx.ErrorGraphs[path] = graph
	}
}

// OpenNodeGraph takes a given mermaid graph and opens it in the system default
// browser.
func OpenNodeGraph(title, path, code, out, graph string) {
	html := fmt.Sprintf(`
        <!DOCTYPE html>
        <html>
        <head>
            <script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
            <script>mermaid.initialize({startOnLoad:true});</script>
			<style>
				.container {
					display: flex;
					flex-direction: column;
					align-items: stretch;
				}
				.row {
					display: flex;
					flex-direction: row;
				}
				.column {
					flex: 50%%;
					padding: 30px;
				}
            </style>
        </head>
        <body>
		<h1>%[4]s</h1>
		<div class="container">
			<div class="mermaid">%[3]s</div>

			<div class="row">
				<div class="column">
					<h3><b>Input</b></h3>
					<pre>%[1]s</pre>
				</div>
				<div class="column">
					<h3><b>Output</b></h3>
					<pre>%[2]s</pre>
				</div>
			</div>
		</div>
        </body>
        </html>
    `, code, out, graph, title)

	os.MkdirAll(path, 0755)
	url := filepath.Join(path, "graph.html")

	err := os.WriteFile(url, []byte(html), 0644)
	if err != nil {
		log.Fatal(err)
	}

	openBrowser(url)
}

// openDebugGraph opens a browser with a graph of the state of the given Vertex
// and all its dependencies that have not completed processing.
// DO NOT DELETE: this is used to insert during debugging of the evaluator
// to inspect a node.
func openDebugGraph(ctx *OpContext, v *Vertex, name string) {
	graph, _ := CreateMermaidGraph(ctx, v, true)
	path := filepath.Join(".debug", "TestX", name)
	OpenNodeGraph(name, path, "in", "out", graph)
}

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

// mermaidContext is used to create a dependency analysis for a node.
type mermaidContext struct {
	ctx *OpContext

	all bool

	hasError bool

	// roots maps the root closeContext of any Vertex to the analysis data
	// for that Vertex.
	roots map[*closeContext]*mermaidVertex

	// processed indicates whether the node in question has been processed
	// by the dependency analysis.
	processed map[*closeContext]bool

	// inConjuncts indicates whether a node is explicitly referenced by
	// a Conjunct. These nodes are visualized with an additional circle.
	inConjuncts map[*closeContext]bool

	// ccID maps a closeContext to a unique ID.
	ccID map[*closeContext]string

	w io.Writer

	// vertices lists an analysis of all nodes related to the analyzed node.
	// The first node is the node being analyzed itself.
	vertices []*mermaidVertex
}

type mermaidVertex struct {
	f     Feature
	w     *bytes.Buffer
	tasks *bytes.Buffer
	intra *bytes.Buffer
}

// CreateMermaidGraph creates an analysis of relations and values involved in
// nodes with unbalanced increments. The graph is in Mermaid format.
func CreateMermaidGraph(ctx *OpContext, v *Vertex, all bool) (graph string, hasError bool) {
	if !DebugDeps {
		return "", false
	}

	buf := &bytes.Buffer{}

	m := &mermaidContext{
		ctx:         ctx,
		roots:       map[*closeContext]*mermaidVertex{},
		processed:   map[*closeContext]bool{},
		inConjuncts: map[*closeContext]bool{},
		ccID:        map[*closeContext]string{},
		w:           indent.New(buf, "   "),
		all:         all,
	}

	io.WriteString(m.w, "graph TD\n")
	io.WriteString(m.w, "   classDef err fill:#e01010,stroke:#000000,stroke-width:3,font-size:medium\n")

	fmt.Fprintf(m.w, "   style %s stroke-width:5\n", m.vertexID(v))
	// Trigger descent on first vertex. This may include other vertices when
	// traversing closeContexts if they have dependencies on such vertices.
	m.vertex(v)

	// Close and flush all collected vertices.
	for i, v := range m.vertices {
		v.closeVertex()
		if i == 0 || len(m.ccID) > 0 {
			m.w.Write(v.w.Bytes())
		}
	}

	return buf.String(), m.hasError
}

// vertex creates a blob of Mermaid graph representing one vertex. It has
// the following shape (where ptr(x) means pointer of x):
//
//		subgraph ptr(v)
//		   %% root note if ROOT has not been decremented.
//		   root((cc1)) -|R|-> ptr(cc1)
//
//		   %% closedness graph dependencies
//		   ptr(cc1)
//		   ptr(cc2) -|P|-> ptr(cc1)
//		   ptr(cc2) -|E|-> ptr(cc1) %% mid schedule
//
//		   %% tasks
//		   subgraph tasks
//		      ptr(cc3)
//		      ptr(cc4)
//		      ptr(cc5)
//		   end
//
//		   %% outstanding tasks and the contexts they depend on
//		   ptr(cc3) -|T|-> ptr(cc2)
//
//		   subgraph notifications
//		      ptr(cc6)
//		      ptr(cc7)
//		   end
//		end
//		%% arcs from nodes to nodes in other vertices
//		ptr(cc1) -|A|-> ptr(cc10)
//		ptr(vx) -|N|-> ptr(cc11)
//
//
//	 A vertex has the following name: path(v); done
//
//	 Each closeContext has the following info: ptr(cc); cc.count
func (m *mermaidContext) vertex(v *Vertex) *mermaidVertex {
	root := v.rootCloseContext()

	vc := m.roots[root]
	if vc != nil {
		return vc
	}

	vc = &mermaidVertex{
		f:     v.Label,
		w:     &bytes.Buffer{},
		intra: &bytes.Buffer{},
	}
	m.vertices = append(m.vertices, vc)

	m.tagReferencedConjuncts(v.Conjuncts)

	m.roots[root] = vc
	w := vc.w

	var status string
	switch {
	case v.status == finalized:
		status = "finalized"
	case v.state == nil:
		status = "ready"
	default:
		status = v.state.scheduler.state.String()
	}
	path := m.vertexPath(v)
	if v.ArcType != ArcMember {
		path += fmt.Sprintf("/%v", v.ArcType)
	}

	fmt.Fprintf(w, "subgraph %s[%s: %s]\n", m.vertexID(v), path, status)

	m.cc(root)

	return vc
}

func (m *mermaidContext) tagReferencedConjuncts(a []Conjunct) {
	for _, c := range a {
		m.inConjuncts[c.CloseInfo.cc] = true

		if g, ok := c.x.(*ConjunctGroup); ok {
			m.tagReferencedConjuncts([]Conjunct(*g))
		}
	}
}

func (v *mermaidVertex) closeVertex() {
	w := v.w

	if v.tasks != nil {
		fmt.Fprintf(v.tasks, "end\n")
		w.Write(indent.Bytes([]byte("   "), v.tasks.Bytes()))
	}

	// TODO: write all notification sources (or is this just the node?)

	fmt.Fprintf(w, "end\n")
}

func (m *mermaidContext) task(d *ccDep) string {
	v := d.dependency.src

	// This must already exist.
	vc := m.vertex(v)

	if vc.tasks == nil {
		vc.tasks = &bytes.Buffer{}
		fmt.Fprintf(vc.tasks, "subgraph %s_tasks[tasks]\n", m.vertexID(v))
	}

	if v != d.task.node.node {
		panic("inconsistent task")
	}
	taskID := fmt.Sprintf("%s_%d", m.vertexID(v), d.taskID)
	var state string
	var completes condition
	if d.task != nil {
		state = d.task.state.String()[:2]
		completes = d.task.completes
	}
	fmt.Fprintf(vc.tasks, "   %s(%d\n%s\n%x)\n", taskID, d.taskID, state, completes)

	if s := d.task.blockedOn; s != nil {
		m.vertex(s.node.node)
		fmt.Fprintf(m.w, "%s_tasks == BLOCKED ==> %s\n", m.vertexID(s.node.node), taskID)
	}

	return taskID
}

func (m *mermaidContext) cc(cc *closeContext) {
	if m.processed[cc] {
		return
	}
	m.processed[cc] = true

	// This must already exist.
	v := m.vertex(cc.src)

	// Dependencies at different scope levels.
	global := m.w
	node := v.w

	for _, d := range cc.dependencies {
		var w io.Writer
		var name, link string

		switch {
		case !d.decremented:
			link = fmt.Sprintf(`--%s-->`, d.kind.String())
		case m.all:
			link = fmt.Sprintf("-. %s .->", d.kind.String()[0:1])
		default:
			continue
		}

		// Only include still outstanding nodes.
		switch d.kind {
		case PARENT:
			w = node
			name = m.pstr(d.dependency)
		case EVAL:
			if cc.Label().IsLet() {
				// Do not show eval links for let nodes, as they never depend
				// on the parent node. Alternatively, link them to the root
				// node instead.
				return
			}
			fallthrough
		case ARC, NOTIFY:
			w = global
			name = m.pstr(d.dependency)

		case TASK:
			w = node
			taskID := m.task(d)
			name = fmt.Sprintf("%s((%d))", taskID, d.taskID)
		case ROOT, INIT: //, EVAL:
			w = node
			src := cc.src
			if v.f != src.Label {
				panic("incompatible labels")
			}
			name = fmt.Sprintf("root_%s", m.vertexID(src))
		}

		if w != nil {
			fmt.Fprintf(w, "   %s %s %s\n", name, link, m.pstr(cc))
		}

		// If the references count is 0, all direct dependencies must have
		// completed as well. In this case, descending into each of them should
		// not end up printing anything. In case of any bugs, these nodes will
		// show up as unattached nodes.

		if dep := d.dependency; dep != nil && dep != cc {
			m.cc(dep)
		}
	}
}

func (m *mermaidContext) vertexPath(v *Vertex) string {
	path := m.ctx.PathToString(v.Path())
	if path == "" {
		return "_"
	}
	return path
}

const sigPtrLen = 6

func (m *mermaidContext) vertexID(v *Vertex) string {
	s := fmt.Sprintf("%p", v)
	return "v" + s[len(s)-sigPtrLen:]
}

func (m *mermaidContext) pstr(cc *closeContext) string {
	if id, ok := m.ccID[cc]; ok {
		return id
	}

	ptr := fmt.Sprintf("%p", cc)
	ptr = ptr[len(ptr)-sigPtrLen:]
	id := fmt.Sprintf("cc%s", ptr)
	m.ccID[cc] = id

	v := m.vertex(cc.src)

	w := v.w

	w.WriteString(id)

	var open, close = "((", "))"
	if m.inConjuncts[cc] {
		open, close = "(((", ")))"
	}

	w.WriteString(open)
	w.WriteString("cc")
	if cc.conjunctCount > 0 {
		fmt.Fprintf(w, " c:%d", cc.conjunctCount)
	}
	w.WriteString("\n")
	w.WriteString(ptr)

	flags := &bytes.Buffer{}
	addFlag := func(test bool, flag byte) {
		if test {
			flags.WriteByte(flag)
		}
	}
	addFlag(cc.isDef, '#')
	addFlag(cc.isEmbed, 'E')
	addFlag(cc.isClosed, 'c')
	addFlag(cc.isClosedOnce, 'C')
	addFlag(cc.hasEllipsis, 'o')
	io.Copy(w, flags)

	w.WriteString(close)

	if cc.conjunctCount > 0 {
		fmt.Fprintf(w, ":::err")
		m.hasError = true
	}

	w.WriteString("\n")

	return id
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
}
