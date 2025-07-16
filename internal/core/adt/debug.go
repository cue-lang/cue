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
	"html/template"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

var (
	// DebugDeps enables dependency tracking for debugging purposes.
	// It is off by default, as it adds a significant overhead.
	//
	// TODO: hook this init CUE_DEBUG, once we have set this up as a single
	// environment variable. For instance, CUE_DEBUG=matchdeps=1.
	DebugDeps = false

	OpenGraphs = false

	// MaxGraphs is the maximum number of debug graphs to be opened. To avoid
	// confusion, a panic will be raised if this number is exceeded.
	MaxGraphs = 10

	numberOpened = 0
)

// OpenNodeGraph takes a given mermaid graph and opens it in the system default
// browser.
func OpenNodeGraph(title, path, code, out, graph string) {
	if !OpenGraphs {
		return
	}
	if numberOpened > MaxGraphs {
		panic("too many debug graphs opened")
	}
	numberOpened++

	err := os.MkdirAll(path, 0777)
	if err != nil {
		log.Fatal(err)
	}
	url := filepath.Join(path, "graph.html")

	w, err := os.Create(url)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	data := struct {
		Title string
		Code  string
		Out   string
		Graph string
	}{
		Title: title,
		Code:  code,
		Out:   out,
		Graph: graph,
	}

	tmpl := template.Must(template.New("").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>{{.Title}}</title>
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
			// ...
		</style>
	</head>
	<body>
		<div class="mermaid">{{.Graph}}</div>
		<div class="row">
			<div class="column">
				<h1><b>Input</b></h1>
				<pre>{{.Code}}</pre>
			</div>
			<div class="column">
				<h1><b>Output</b></h1>
				<pre>{{.Out}}</pre>
			</div>
		</div>
	</body>
	</html>
`))

	err = tmpl.Execute(w, data)
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
	if !OpenGraphs {
		return
	}
	graph, _ := CreateMermaidGraph(ctx, v, true)
	path := filepath.Join(".debug", "TestX", name, fmt.Sprintf("%v", v.Path()))
	OpenNodeGraph(name, path, "in", "out", graph)
}

// mermaidContext is used to create a dependency analysis for a node.
type mermaidContext struct {
	ctx *OpContext
	v   *Vertex

	all bool

	hasError bool

	// roots maps a Vertex to the analysis data for that Vertex.
	roots map[*Vertex]*mermaidVertex

	w io.Writer

	// vertices lists an analysis of all nodes related to the analyzed node.
	// The first node is the node being analyzed itself.
	vertices []*mermaidVertex
}

type mermaidVertex struct {
	vertex    *Vertex
	f         Feature
	w         *bytes.Buffer
	tasks     *bytes.Buffer
	intra     *bytes.Buffer
	processed bool
}

// CreateMermaidGraph creates an analysis of relations and values involved in
// nodes with unbalanced increments. The graph is in Mermaid format.
func CreateMermaidGraph(ctx *OpContext, v *Vertex, all bool) (graph string, hasError bool) {
	buf := &strings.Builder{}

	m := &mermaidContext{
		ctx:   ctx,
		v:     v,
		roots: map[*Vertex]*mermaidVertex{},
		w:     buf,
		all:   all,
	}

	io.WriteString(m.w, "graph TD\n")
	io.WriteString(m.w, "   classDef err fill:#e01010,stroke:#000000,stroke-width:3,font-size:medium\n")
	fmt.Fprintf(m.w, "   title[<b>%v</b>]\n", ctx.disjunctInfo())

	indent(m.w, 1)
	fmt.Fprintf(m.w, "style %s stroke-width:5\n\n", m.vertexID(v))
	// Trigger descent on first vertex. This may include other vertices when
	// traversing closeContexts if they have dependencies on such vertices.
	m.vertex(v, true)

	// get parent context, if there is relevant closedness information.
	root := v.Parent
	for p := root; p != nil; p = p.Parent {
		n := p.state
		if n == nil {
			continue
		}
		if len(n.reqDefIDs) > 0 {
			root = p.Parent
		}
	}
	for p := v.Parent; p != root; p = p.Parent {
		m.vertex(p, true) // only render relevant child
	}

	// Close and flush all collected vertices.
	for _, v := range m.vertices {
		v.closeVertex()
		m.w.Write(v.w.Bytes())
	}

	s := buf.String()

	return s, m.hasError
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
func (m *mermaidContext) vertex(v *Vertex, recursive bool) *mermaidVertex {
	vc := m.roots[v]
	if vc != nil {
		return vc
	}

	vc = &mermaidVertex{
		vertex: v,
		f:      v.Label,
		w:      &bytes.Buffer{},
		intra:  &bytes.Buffer{},
	}
	m.vertices = append(m.vertices, vc)

	m.roots[v] = vc
	w := vc.w

	var status string
	switch {
	case v.Status() == finalized:
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

	indentOnNewline(w, 1)
	fmt.Fprintf(w, "subgraph %s[%s: %s]\n", m.vertexID(v), path, status)

	m.vertexInfo(vc, recursive)

	return vc
}

func (v *mermaidVertex) closeVertex() {
	w := v.w

	if v.tasks != nil {
		indent(v.tasks, 2)
		fmt.Fprintf(v.tasks, "end\n")
		w.Write(v.tasks.Bytes())
	}

	// TODO: write all notification sources (or is this just the node?)

	indent(w, 1)
	fmt.Fprintf(w, "\nend\n")
}

func (m *mermaidContext) task(vc *mermaidVertex, t *task, id int) string {
	v := vc.vertex

	if vc.tasks == nil {
		vc.tasks = &bytes.Buffer{}
		indentOnNewline(vc.tasks, 2)
		fmt.Fprintf(vc.tasks, "subgraph %s_tasks[tasks]\n", m.vertexID(v))
	}

	if t != nil && v != t.node.node {
		panic("inconsistent task")
	}
	taskID := fmt.Sprintf("%s_%d", m.vertexID(v), id)
	var state string
	var completes condition
	var kind string
	if t != nil {
		state = t.state.String()[:2]
		completes = t.completes
		kind = t.run.name
	}
	indentOnNewline(vc.tasks, 3)
	fmt.Fprintf(vc.tasks, "%s(%d", taskID, id)
	indentOnNewline(vc.tasks, 4)
	io.WriteString(vc.tasks, state)
	indentOnNewline(vc.tasks, 4)
	io.WriteString(vc.tasks, kind)
	indentOnNewline(vc.tasks, 4)
	fmt.Fprintf(vc.tasks, "%x)\n", completes)

	if s := t.blockedOn; s != nil {
		m.vertex(s.node.node, false)
		fmt.Fprintf(m.w, "%s_tasks == BLOCKED ==> %s\n", m.vertexID(s.node.node), taskID)
	}

	return taskID
}

func (m *mermaidContext) vertexInfo(vc *mermaidVertex, recursive bool) {
	if vc.processed {
		return
	}
	vc.processed = true

	v := vc.vertex

	// This must already exist.

	// Dependencies at different scope levels.
	global := m.w
	node := vc.w

	if s := v.state; s != nil {
		for i, t := range s.tasks {
			taskID := m.task(vc, t, i)
			name := fmt.Sprintf("%s((%d))", taskID, 1)
			_ = name
			// 		dst := m.pstr(cc)
			// 		indent(w, indentLevel)
			// 		fmt.Fprintf(w, "%s %s %s\n", name, link, dst)
		}
	}

	indentOnNewline(node, 2)
	if n := v.state; n != nil {
		for i, d := range n.reqDefIDs {
			indentOnNewline(node, 2)
			var id any = d.id
			if d.v != nil && d.v.ClosedNonRecursive && d.id == 0 {
				id = "once"
			}
			reqID := fmt.Sprintf("%s_req_%d", m.vertexID(v), i)
			arrow := "%s == R ==> %s\n"
			format := "%s((%d%s))\n"
			if d.ignore {
				arrow = "%s -. R .-> %s\n"
				format = "%s((<s><i>%d%si</i></s>))\n"
			}
			flags := ""
			if d.kind != 0 {
				flags += d.kind.String()
			}
			if d.embed != 0 {
				flags += fmt.Sprintf("-%de", d.embed)
			}
			if d.parent != 0 {
				flags += fmt.Sprintf("-%dp", d.parent)
			}

			fmt.Fprintf(node, format, reqID, id, flags)
			m.vertex(d.v, false)
			fmt.Fprintf(global, arrow, reqID, m.vertexID(d.v))
		}
		indentOnNewline(node, 2)

		// fmt.Fprintf(node, "subgraph %s_conjuncts[conjunctInfo]\n", m.vertexID(v))
		fmt.Fprintf(node, "subgraph %s_conjuncts[conjuncts]\n", m.vertexID(v))
		for i, conj := range n.conjunctInfo {
			indentOnNewline(node, 3)
			kind := conj.kind.String()
			if kind == "_|_" {
				kind = "error"
			}
			x := conj.flags
			flags := ""
			if x != 0 {
				flags = " "
			}
			if x&cHasTop != 0 {
				flags += "_"
			}
			if x&cHasStruct != 0 {
				flags += "s"
			}
			if x&cHasEllipsis != 0 {
				flags += "."
			}
			if x&cHasOpenValidator != 0 {
				flags += "o"
			}
			var scope string
			if conj.embed != 0 {
				scope = fmt.Sprintf("-%d", conj.embed)
			}
			fmt.Fprintf(node, "%s_conj_%d((%v\n%d%s%s))", m.vertexID(v), i, kind, conj.id, scope, flags)
		}
		indentOnNewline(node, 2)
		fmt.Fprintln(node, "end")

		if len(n.replaceIDs) > 0 {
			fmt.Fprintf(node, "subgraph %s_drop[replace]\n", m.vertexID(v))
			for i, r := range n.replaceIDs {
				indentOnNewline(node, 3)
				dropID := fmt.Sprintf("%s_drop_%d", m.vertexID(v), i)
				flags := ""
				fmt.Fprintf(node, "%s((%d->%d%s))\n", dropID, r.from, r.to, flags)
			}
			indentOnNewline(node, 2)
			// fmt.Fprintf(node, "end\n")
			fmt.Fprintln(node, "end")
		}
	}

	if v.Parent != nil {
		m.vertex(v.Parent, false) // ensure the arc is also processed
		indentOnNewline(node, 2)
		fmt.Fprintf(global, "%s --> %s\n", m.vertexID(v.Parent), m.vertexID(v))
	}
	if recursive {
		for _, arc := range v.Arcs {
			m.vertex(arc, true) // ensure the arc is also processed
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

func indentOnNewline(w io.Writer, level int) {
	w.Write([]byte{'\n'})
	indent(w, level)
}

func indent(w io.Writer, level int) {
	for i := 0; i < level; i++ {
		io.WriteString(w, "   ")
	}
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
	go cmd.Wait()
}
