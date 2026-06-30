// Copyright 2026 CUE Authors
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

package eval_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/unstable/lsp/eval"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

// graphTestCase is a single case in the graph API test table. Each
// case builds an [eval.Evaluator] from archive (plus one evaluator
// per entry of deps, wired up as importable packages), and then runs
// check, which expresses the case's expectations using the
// [graphTest] helpers.
type graphTestCase struct {
	name string
	// archive is a txtar archive containing the files of the package
	// under test.
	archive string
	// deps maps import paths to txtar archives of packages that the
	// package under test imports.
	deps map[string]string
	// check expresses the expectations for this case.
	check func(t *graphTest)
}

type graphTestCases []graphTestCase

func (tcs graphTestCases) run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			deps := make(map[string]*eval.Evaluator, len(tc.deps))
			for importPath, archive := range tc.deps {
				ip := ast.ParseImportPath(importPath).Canonical()
				deps[ip.String()] = eval.New(eval.Config{IP: ip}, parseArchive(t, archive)...)
			}
			cfg := eval.Config{
				IP: ast.ParseImportPath("example.test/p").Canonical(),
				ForPackage: func(ip ast.ImportPath) *eval.Evaluator {
					return deps[ip.String()]
				},
			}
			ev := eval.New(cfg, parseArchive(t, tc.archive)...)
			tc.check(&graphTest{T: t, ev: ev, deps: deps})
		})
	}
}

func parseArchive(t *testing.T, archive string) []*ast.File {
	t.Helper()
	ar := txtar.Parse([]byte(archive))
	qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

	var files []*ast.File
	for _, fh := range ar.Files {
		f, err := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
		qt.Assert(t, qt.IsNil(err))
		f.Pos().File().SetContent(fh.Data)
		files = append(files, f)
	}
	return files
}

// graphTest is the fixture passed to a [graphTestCase]'s check
// callback. It provides navigation and assertion helpers for
// expressing expectations about the graph.
type graphTest struct {
	*testing.T
	ev   *eval.Evaluator
	deps map[string]*eval.Evaluator
}

// root returns the root node of the package under test.
func (gt *graphTest) root() *eval.Node {
	return gt.ev.Root()
}

// dep returns the evaluator for the given import path, which must
// appear in the case's deps.
func (gt *graphTest) dep(importPath string) *eval.Evaluator {
	gt.Helper()
	ev := gt.deps[ast.ParseImportPath(importPath).Canonical().String()]
	qt.Assert(gt, qt.IsNotNil(ev))
	return ev
}

// field navigates from the package root through the given dotted path
// of field names, asserting that every step exists.
func (gt *graphTest) field(path string) *eval.Node {
	gt.Helper()
	return gt.fieldOf(gt.root(), path)
}

// fieldOf navigates from n through the given dotted path of field
// names, asserting that every step exists.
func (gt *graphTest) fieldOf(n *eval.Node, path string) *eval.Node {
	gt.Helper()
	for name := range strings.SplitSeq(path, ".") {
		n = n.Field(name)
		qt.Assert(gt, qt.IsNotNil(n), qt.Commentf("field %q of path %q", name, path))
	}
	return n
}

// declsOfKind returns n's decls of the given kind, in order.
func (gt *graphTest) declsOfKind(n *eval.Node, kind eval.DeclKind) []*eval.Decl {
	var decls []*eval.Decl
	for d := range n.Decls() {
		if d.Kind() == kind {
			decls = append(decls, d)
		}
	}
	return decls
}

// soleDecl asserts that n has exactly one decl of the given kind, and
// returns it.
func (gt *graphTest) soleDecl(n *eval.Node, kind eval.DeclKind) *eval.Decl {
	gt.Helper()
	decls := gt.declsOfKind(n, kind)
	qt.Assert(gt, qt.HasLen(decls, 1))
	return decls[0]
}

// checkFields asserts the names yielded by n.Fields(), which are
// expected to be in lexical order.
func (gt *graphTest) checkFields(n *eval.Node, want ...string) {
	gt.Helper()
	names := []string{}
	for name := range n.Fields() {
		names = append(names, name)
	}
	qt.Assert(gt, qt.DeepEquals(names, notNil(want)))
}

// checkNodeSetFields asserts the names yielded by ns.Fields().
func (gt *graphTest) checkNodeSetFields(ns eval.NodeSet, want ...string) {
	gt.Helper()
	names := []string{}
	for name := range ns.Fields() {
		names = append(names, name)
	}
	qt.Assert(gt, qt.DeepEquals(names, notNil(want)))
}

// checkDeclKinds asserts the kinds of n's decls with bag semantics:
// each kind must occur the expected number of times, but the order of
// the decls is irrelevant.
func (gt *graphTest) checkDeclKinds(n *eval.Node, want ...eval.DeclKind) {
	gt.Helper()
	kinds := []eval.DeclKind{}
	for d := range n.Decls() {
		kinds = append(kinds, d.Kind())
	}
	slices.Sort(kinds)
	want = notNil(want)
	slices.Sort(want)
	qt.Assert(gt, qt.DeepEquals(kinds, want))
}

// checkDeclFields asserts the names yielded by d.Fields().
func (gt *graphTest) checkDeclFields(d *eval.Decl, want ...string) {
	gt.Helper()
	names := []string{}
	for name := range d.Fields() {
		names = append(names, name)
	}
	qt.Assert(gt, qt.DeepEquals(names, notNil(want)))
}

// checkNodes asserts that got holds exactly the given nodes. A
// NodeSet's member order is unspecified, so got is first normalized
// by sorting on source position: list want in source-position
// order. Nodes are canonical, so identity comparison is the correct
// notion of equality.
func (gt *graphTest) checkNodes(got eval.NodeSet, want ...*eval.Node) {
	gt.Helper()
	describe := func(ns eval.NodeSet) []string {
		var out []string
		for _, n := range ns {
			if path, ok := n.FieldPath(); ok {
				out = append(out, fmt.Sprintf("%p(%s)", n, strings.Join(path, ".")))
			} else {
				out = append(out, fmt.Sprintf("%p(anon)", n))
			}
		}
		return out
	}
	slices.SortFunc(got, func(a, b *eval.Node) int {
		return nodeSortPos(a).Compare(nodeSortPos(b))
	})
	qt.Assert(gt, qt.IsTrue(slices.Equal(got, want)),
		qt.Commentf("got %v, want %v", describe(got), describe(eval.NodeSet(want))))
}

func nodeSortPos(n *eval.Node) token.Pos {
	pos := token.NoPos
	for d := range n.Decls() {
		v := d.Value()
		if v == nil {
			// e.g. a package clause, or a bare ellipsis.
			continue
		}
		if p := v.Pos(); p.HasAbsPos() && p.Compare(pos) < 0 {
			pos = p
		}
	}
	return pos
}

// checkResolve asserts that resolving el via d yields exactly the
// given nodes, listed in source-position order (see checkNodes).
func (gt *graphTest) checkResolve(d *eval.Decl, el ast.Node, want ...*eval.Node) {
	gt.Helper()
	gt.checkNodes(d.Resolve(el), want...)
}

// checkKey asserts the rendered form of d.Key(): the key's text,
// filename and line, or "" for a nil key.
func (gt *graphTest) checkKey(d *eval.Decl, want string) {
	gt.Helper()
	qt.Assert(gt, qt.Equals(renderKeyNode(d.Key()), want))
}

// checkValue asserts the rendered form of d.Value(): its formatted
// source, File(name) for files, or "" for a nil value.
func (gt *graphTest) checkValue(d *eval.Decl, want string) {
	gt.Helper()
	qt.Assert(gt, qt.Equals(renderValueNode(d.Value()), want))
}

// checkDocs asserts the joined text of d.DocComments().
func (gt *graphTest) checkDocs(d *eval.Decl, want string) {
	gt.Helper()
	qt.Assert(gt, qt.Equals(joinDocComments(d.DocComments()), want))
}

// notNil converts a nil slice (from an empty variadic) to an empty
// one, so that expectations of emptiness compare equal.
func notNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// findNode returns the first node of type T within root, in a
// pre-order walk, that satisfies pred.
func findNode[T ast.Node](t testing.TB, root ast.Node, pred func(T) bool) T {
	t.Helper()
	var result T
	found := false
	ast.Walk(root, func(n ast.Node) bool {
		if found {
			return false
		}
		if tn, ok := n.(T); ok && pred(tn) {
			result = tn
			found = true
			return false
		}
		return true
	}, nil)
	qt.Assert(t, qt.IsTrue(found))
	return result
}

func renderKeyNode(n ast.Node) string {
	if n == nil {
		return ""
	}
	var text string
	switch k := n.(type) {
	case *ast.Ident:
		text = k.Name
	case *ast.BasicLit:
		text = k.Value
	default:
		text = fmt.Sprintf("%T", n)
	}
	pos := n.Pos().Position()
	return fmt.Sprintf("%s @ %s:%d", text, pos.Filename, pos.Line)
}

func renderValueNode(n ast.Node) string {
	if n == nil {
		return ""
	}
	if f, ok := n.(*ast.File); ok {
		return fmt.Sprintf("File(%s)", f.Filename)
	}
	b, err := format.Node(n)
	if err != nil {
		return fmt.Sprintf("<err:%v>", err)
	}
	return strings.TrimSpace(string(b))
}

func joinDocComments(groups []*ast.CommentGroup) string {
	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, c := range g.List {
			if j > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func TestGraph(t *testing.T) {
	graphTestCases{
		{
			name: "RootAndFileEmbeddings",
			archive: `-- a.cue --
// Package p is a test package.
package p

x: 3
-- b.cue --
package p

y

y: {z: 4}
`,
			check: func(t *graphTest) {
				root := t.root()

				// The root node is canonical and anonymous.
				qt.Assert(t, qt.Equals(t.ev.Root(), root))
				qt.Assert(t, qt.Equals(root.Evaluator(), t.ev))
				qt.Assert(t, qt.Equals(root.Name(), ""))
				qt.Assert(t, qt.IsNil(root.Parent()))
				path, ok := root.FieldPath()
				qt.Assert(t, qt.IsTrue(ok))
				qt.Assert(t, qt.HasLen(path, 0))

				// Fields across all files are merged at the root.
				t.checkFields(root, "x", "y")
				qt.Assert(t, qt.Equals(root.Field("y"), t.field("y")))

				// One DeclFile per file, plus the embedding of y in
				// b.cue. The package clauses are not decls of the root.
				t.checkDeclKinds(root, eval.DeclFile, eval.DeclFile, eval.DeclEmbedding)
				fileDecls := t.declsOfKind(root, eval.DeclFile)
				t.checkValue(fileDecls[0], "File(a.cue)")
				t.checkValue(fileDecls[1], "File(b.cue)")
				qt.Assert(t, qt.IsNil(fileDecls[0].Key()))
				qt.Assert(t, qt.Equals(fileDecls[0].Node(), root))

				// The file-level embedding is visible as a decl of the
				// root, is flagged as embedded, and its value resolves
				// to the y field's node.
				embDecl := t.soleDecl(root, eval.DeclEmbedding)
				qt.Assert(t, qt.IsTrue(embDecl.Kind().Embedded()))
				t.checkValue(embDecl, "y")
				t.checkResolve(embDecl, embDecl.Value(), t.field("y"))

				// Expanding the root follows the embedding.
				qt.Assert(t, qt.IsTrue(slices.Contains(root.Expand(), t.field("y"))))
			},
		},

		{
			name: "PackageClauses",
			archive: `-- a.cue --
// Package p is a test package.
package p

x: 3
-- b.cue --
package p
`,
			check: func(t *graphTest) {
				// Package clauses live on their own node, one decl per
				// clause, carrying the package docs.
				pkg := t.ev.PackageClauses()
				qt.Assert(t, qt.Equals(t.ev.PackageClauses(), pkg))
				t.checkFields(pkg)
				pkgDecls := t.declsOfKind(pkg, eval.DeclPackage)
				qt.Assert(t, qt.HasLen(pkgDecls, 2))
				t.checkKey(pkgDecls[0], "p @ a.cue:2")
				t.checkDocs(pkgDecls[0], "// Package p is a test package.")
				t.checkKey(pkgDecls[1], "p @ b.cue:1")
				t.checkDocs(pkgDecls[1], "")
			},
		},

		{
			name: "OwnFieldsVersusReferences",
			archive: `-- a.cue --
package p

x: {a: 1} & {b: 2}
x: {c: 3, d}
d: e: 4
`,
			check: func(t *graphTest) {
				t.checkFields(t.root(), "d", "x")

				// Conjunction operands, embedded struct literals and
				// duplicate declarations all count as x's own fields;
				// the embedded reference d does not contribute its e.
				x := t.field("x")
				t.checkFields(x, "a", "b", "c")
				qt.Assert(t, qt.IsNil(x.Field("e")))

				// e is reachable via expansion, and its provenance is d.
				expanded := x.Expand()
				t.checkNodeSetFields(expanded, "a", "b", "c", "e")
				t.checkNodes(expanded.Field("e"), t.field("d.e"))
				qt.Assert(t, qt.Equals(t.field("d.e").Parent(), t.field("d")))
			},
		},

		{
			name: "UnmodelledFieldForms",
			archive: `-- a.cue --
package p

k: "kk"
[string]: pat: 1
("dy"+"n"): dyn: 1
"\(k)": interp: 1
opt?: 1
req!: 2
`,
			check: func(t *graphTest) {
				root := t.root()

				// Pattern constraints, dynamic fields and interpolated
				// field names contribute no fields anywhere: not to
				// Fields, and not via expansion either.
				t.checkFields(root, "k", "opt", "req")
				for _, name := range []string{"pat", "dyn", "interp"} {
					t.checkNodes(root.Expand().Field(name))
				}

				// Optionality is not modelled: opt?: 1 and req!: 2
				// declare plain fields, indistinguishable from opt: 1
				// and req: 2.
				optDecl := t.soleDecl(t.field("opt"), eval.DeclField)
				qt.Assert(t, qt.IsFalse(optDecl.Kind().Embedded()))
				t.checkKey(optDecl, "opt @ a.cue:7")
				reqDecl := t.soleDecl(t.field("req"), eval.DeclField)
				qt.Assert(t, qt.IsFalse(reqDecl.Kind().Embedded()))
				t.checkKey(reqDecl, "req @ a.cue:8")
			},
		},

		{
			// NodeSet.Decls yields the declarations of every member of
			// the set, each exactly once — e.g. gathering the docs of
			// every declaration site of a merged field.
			name: "NodeSetDecls",
			archive: `-- a.cue --
package p

x: {
	// a as declared on x.
	a: 1
}
x: y

// y's doc.
y: {
	// a as declared on y.
	a: 2
}
`,
			check: func(t *graphTest) {
				x, y := t.field("x"), t.field("y")

				// The per-name set of the expanded view holds both
				// declaration sites of a, and its Decls carry both
				// sites' docs: the hover use-case.
				as := x.Expand().Field("a")
				t.checkNodes(as, x.Field("a"), y.Field("a"))
				docs := []string{}
				for d := range as.Decls() {
					docs = append(docs, joinDocComments(d.DocComments()))
				}
				slices.Sort(docs)
				qt.Assert(t, qt.DeepEquals(docs, []string{
					"// a as declared on x.",
					"// a as declared on y.",
				}))

				// The set's decls are the union of its members' decls,
				// in member order...
				want := slices.Collect(x.Decls())
				want = append(want, slices.Collect(y.Decls())...)
				got := slices.Collect(eval.NodeSet{x, y}.Decls())
				qt.Assert(t, qt.IsTrue(slices.Equal(got, want)))

				// ...and a member repeated in a hand-built set does not
				// repeat its decls.
				got = slices.Collect(eval.NodeSet{x, x, y}.Decls())
				qt.Assert(t, qt.IsTrue(slices.Equal(got, want)))
			},
		},

		{
			name: "ConjunctsAndDisjuncts",
			archive: `-- a.cue --
package p

conj: {a: 1} & {b: 2}
disj: {c: 1} | {d: 2}
`,
			check: func(t *graphTest) {
				// Conjunction: one DeclField for the field itself
				// (value: the whole binary expression), one
				// DeclConjunct per operand. All three contribute to the
				// same node; per-decl Fields tells the operands apart.
				conj := t.field("conj")
				t.checkDeclKinds(conj, eval.DeclField, eval.DeclConjunct, eval.DeclConjunct)
				t.checkFields(conj, "a", "b")
				conjDecls := slices.Collect(conj.Decls())
				qt.Assert(t, qt.IsFalse(conjDecls[0].Kind().Embedded()))
				t.checkValue(conjDecls[0], "{a: 1} & {b: 2}")
				t.checkDeclFields(conjDecls[0])
				for i, want := range []string{"a", "b"} {
					d := conjDecls[i+1]
					qt.Assert(t, qt.IsTrue(d.Kind().Embedded()))
					qt.Assert(t, qt.Equals(d.Node(), conj))
					qt.Assert(t, qt.IsNil(d.Key()))
					t.checkDeclFields(d, want)
				}

				// Disjunction: same shape, kind DeclDisjunct. The
				// merged Fields view does not distinguish branches (a
				// MAY-analysis); the per-decl view recovers them.
				disj := t.field("disj")
				t.checkDeclKinds(disj, eval.DeclField, eval.DeclDisjunct, eval.DeclDisjunct)
				t.checkFields(disj, "c", "d")
				disjDecls := slices.Collect(disj.Decls())
				t.checkDeclFields(disjDecls[1], "c")
				t.checkDeclFields(disjDecls[2], "d")
			},
		},

		{
			name: "EmbeddedReferenceInStruct",
			archive: `-- a.cue --
package p

emb: {sub, e: 5}
sub: {f: 1}
`,
			check: func(t *graphTest) {
				// The embedded reference is a decl of kind
				// DeclEmbedding contributing no fields of its own:
				// sub's fields are only reachable via expansion.
				emb := t.field("emb")
				t.checkDeclKinds(emb, eval.DeclField, eval.DeclEmbedding)
				t.checkFields(emb, "e")
				embDecl := t.soleDecl(emb, eval.DeclEmbedding)
				t.checkValue(embDecl, "sub")
				t.checkResolve(embDecl, embDecl.Value(), t.field("sub"))
				t.checkNodeSetFields(emb.Expand(), "e", "f")
			},
		},

		{
			name: "DefaultsAndComprehensions",
			archive: `-- a.cue --
package p

def: *{g: 1} | {h: 2}
comp: {if true {i: 1}}
`,
			check: func(t *graphTest) {
				// Default: the *{g: 1} operand appears both as a
				// DeclDisjunct (the whole unary expression) and as a
				// DeclDefault (its operand).
				def := t.field("def")
				t.checkDeclKinds(def,
					eval.DeclField, eval.DeclDisjunct, eval.DeclDisjunct, eval.DeclDefault)
				defDecl := t.soleDecl(def, eval.DeclDefault)
				t.checkValue(defDecl, "{g: 1}")
				t.checkDeclFields(defDecl, "g")
				t.checkFields(def, "g", "h")

				// Comprehension: the body is a DeclComprehension. As a
				// MAY-analysis the evaluator includes its fields
				// regardless of the condition.
				comp := t.field("comp")
				t.checkDeclKinds(comp, eval.DeclField, eval.DeclComprehension)
				t.checkFields(comp, "i")
				compDecl := t.soleDecl(comp, eval.DeclComprehension)
				qt.Assert(t, qt.IsTrue(compDecl.Kind().Embedded()))
				t.checkDeclFields(compDecl, "i")
			},
		},

		{
			name: "FieldsInEmbeddedStructs",
			archive: `-- a.cue --
package p

dup: 3
{dup: 4}
`,
			check: func(t *graphTest) {
				// A field declared inside an embedded struct literal is
				// itself a plain, non-embedded DeclField: the embedding
				// is a separate decl, of the root node.
				t.checkDeclKinds(t.root(), eval.DeclFile, eval.DeclEmbedding)
				dup := t.field("dup")
				dupDecls := slices.Collect(dup.Decls())
				qt.Assert(t, qt.HasLen(dupDecls, 2))
				for _, d := range dupDecls {
					qt.Assert(t, qt.Equals(d.Kind(), eval.DeclField))
					qt.Assert(t, qt.IsFalse(d.Kind().Embedded()))
				}
				t.checkValue(dupDecls[0], "3")
				t.checkValue(dupDecls[1], "4")
			},
		},

		{
			name: "ExpandReferences",
			archive: `-- a.cue --
package p

x: {a: 1}
x: y
y: {b: 2}
z: y
`,
			check: func(t *graphTest) {
				x, y, z := t.field("x"), t.field("y"), t.field("z")

				// y is included via the reference; the set is ordered
				// by source position.
				t.checkNodes(x.Expand(), x, y)
				// x's own fields lack b; the expanded view has it.
				t.checkFields(x, "a")
				t.checkNodeSetFields(x.Expand(), "a", "b")
				// Nodes are canonical, so different routes to y's b
				// meet.
				t.checkNodes(x.Expand().Field("b"), t.field("y.b"))
				// Expansion is transitive. Position ordering places y
				// (line 5) before the receiver z (line 6).
				t.checkNodes(z.Expand(), y, z)
			},
		},

		{
			name: "ExpandProvenance",
			archive: `-- a.cue --
package p

x: {a: 1}
x: y
y: {a: 2}
`,
			check: func(t *graphTest) {
				x, y := t.field("x"), t.field("y")

				// Both declaration sites of a survive in the merged
				// view, and Parent tells them apart.
				as := x.Expand().Field("a")
				t.checkNodes(as, x.Field("a"), y.Field("a"))
				qt.Assert(t, qt.Equals(as[0].Parent(), x))
				qt.Assert(t, qt.Equals(as[1].Parent(), y))
			},
		},

		{
			name: "ExpandImpliedEdges",
			archive: `-- a.cue --
package p

a: b: c: 3
x: a
x: b: d: 4
`,
			check: func(t *graphTest) {
				// No expression refers from x.b to a.b, but unification
				// of x with a implies the inclusion, and expansion
				// reports it.
				xb := t.field("x.b")
				qt.Assert(t, qt.IsTrue(slices.Contains(xb.Expand(), t.field("a.b"))))
				t.checkNodeSetFields(xb.Expand(), "c", "d")
			},
		},

		{
			name: "ResolvePathComponents",
			archive: `-- a.cue --
package p

x: {a: 1, b: 2}
y: x
z: x.a
w: {x.b, c: 3}
o: {p: {q: 4}}
dp: o.p.q
l: [{li: 5}]
il: l[0].li
pv: (x.a)
dv: *x | {n: 1}
pdv: *(x) | {n2: 2}
inline: {ia: _}.ia
`,
			check: func(t *graphTest) {
				x := t.field("x")

				// A reference value resolves to the referenced node.
				yDecl := t.soleDecl(t.field("y"), eval.DeclField)
				t.checkResolve(yDecl, yDecl.Value(), x)

				// Each component of a path resolves separately: the
				// whole selector (and its final ident) to x.a, the
				// leading ident to x.
				zDecl := t.soleDecl(t.field("z"), eval.DeclField)
				sel := zDecl.Value().(*ast.SelectorExpr)
				t.checkResolve(zDecl, sel, x.Field("a"))
				t.checkResolve(zDecl, sel.Sel, x.Field("a"))
				t.checkResolve(zDecl, sel.X, x)

				// In a longer path, every constituent expression
				// resolves: a selector expression resolves as its final
				// component, so the interior selector o.p resolves to
				// the o.p prefix, exactly as the ident p within it does.
				dpDecl := t.soleDecl(t.field("dp"), eval.DeclField)
				dpSel := dpDecl.Value().(*ast.SelectorExpr)
				inner := dpSel.X.(*ast.SelectorExpr)
				t.checkResolve(dpDecl, dpSel, t.field("o.p.q"))
				t.checkResolve(dpDecl, dpSel.Sel, t.field("o.p.q"))
				t.checkResolve(dpDecl, inner, t.field("o.p"))
				t.checkResolve(dpDecl, inner.Sel, t.field("o.p"))
				t.checkResolve(dpDecl, inner.X, t.field("o"))

				// Likewise an index expression resolves as its Index:
				// the interior l[0] resolves to the (anonymous) list
				// element.
				ilDecl := t.soleDecl(t.field("il"), eval.DeclField)
				ilSel := ilDecl.Value().(*ast.SelectorExpr)
				ilIndex := ilSel.X.(*ast.IndexExpr)
				element := slices.Collect(t.field("l").ListElements())[0]
				t.checkResolve(ilDecl, ilIndex, element)
				t.checkResolve(ilDecl, ilSel, element.Field("li"))

				// Only path elements resolve: wrapper expressions such
				// as parentheses or a unary * default marker do not.
				// Walk inside them and resolve the path expression or
				// ident within.
				pvDecl := t.soleDecl(t.field("pv"), eval.DeclField)
				qt.Assert(t, qt.IsNil(pvDecl.Resolve(pvDecl.Value())))
				pvParen := pvDecl.Value().(*ast.ParenExpr)
				t.checkResolve(pvDecl, pvParen.X, x.Field("a"))
				dvDecl := t.soleDecl(t.field("dv"), eval.DeclField)
				dvDefault := findNode(t, dvDecl.Value(),
					func(*ast.UnaryExpr) bool { return true })
				qt.Assert(t, qt.IsNil(dvDecl.Resolve(dvDefault)))
				t.checkResolve(dvDecl, dvDefault.X, x)
				pdvDecl := t.soleDecl(t.field("pdv"), eval.DeclField)
				pdvDefault := findNode(t, pdvDecl.Value(),
					func(*ast.UnaryExpr) bool { return true })
				qt.Assert(t, qt.IsNil(pdvDecl.Resolve(pdvDefault)))
				pdvParen := pdvDefault.X.(*ast.ParenExpr)
				t.checkResolve(pdvDecl, pdvParen.X, x)

				// A path rooted at an inline expression: the struct is
				// not a path element and does not resolve; the ident ia
				// and the whole expression yield the node for the field
				// within the struct, and the struct's own anonymous
				// node is that node's Parent.
				inlineDecl := t.soleDecl(t.field("inline"), eval.DeclField)
				inSel := inlineDecl.Value().(*ast.SelectorExpr)
				qt.Assert(t, qt.IsNil(inlineDecl.Resolve(inSel.X)))
				resolved := inlineDecl.Resolve(inSel)
				qt.Assert(t, qt.HasLen(resolved, 1))
				inA := resolved[0]
				t.checkResolve(inlineDecl, inSel.Sel, inA)
				structNode := inA.Parent()
				qt.Assert(t, qt.Equals(structNode.Name(), ""))
				_, addressable := structNode.FieldPath()
				qt.Assert(t, qt.IsFalse(addressable))
				structDecl := t.soleDecl(structNode, eval.DeclExpression)
				t.checkValue(structDecl, "{ia: _}")
				// Nodes are canonical: descending from the struct node
				// meets the resolution.
				qt.Assert(t, qt.Equals(structNode.Field("ia"), inA))

				// Elements are identified by node identity: an
				// equivalent, but not identical, ast.Node resolves to
				// nothing, even when it carries the right position.
				selIdent := sel.Sel.(*ast.Ident)
				clone := ast.NewIdent(selIdent.Name)
				clone.NamePos = selIdent.NamePos
				qt.Assert(t, qt.IsNil(zDecl.Resolve(clone)))

				// Elements found by walking a decl's value resolve too:
				// here the selector embedded within w's struct literal.
				wDecl := t.soleDecl(t.field("w"), eval.DeclField)
				embSel := findNode(t, wDecl.Value(), func(*ast.SelectorExpr) bool { return true })
				t.checkResolve(wDecl, embSel, x.Field("b"))

				// A field declaration's key resolves to the node it
				// declares.
				t.checkResolve(yDecl, yDecl.Key(), yDecl.Node())

				// Elements that are not tracked by the evaluator do not
				// resolve.
				qt.Assert(t, qt.IsNil(yDecl.Resolve(nil)))
				aDecl := t.soleDecl(x.Field("a"), eval.DeclField)
				qt.Assert(t, qt.IsNil(aDecl.Resolve(aDecl.Value()))) // a BasicLit
			},
		},

		{
			name: "ResolveAliasesAndLets",
			archive: `-- a.cue --
package p

x: {a: 1}
let l = x
m: l
al=n: {q: 1}
r: al.q
[ka=string]: kav: ka
`,
			check: func(t *graphTest) {
				x := t.field("x")

				// A use of a let binding resolves to the let's
				// anonymous node; expanding sees through it.
				mDecl := t.soleDecl(t.field("m"), eval.DeclField)
				resolved := mDecl.Resolve(mDecl.Value())
				qt.Assert(t, qt.HasLen(resolved, 1))
				letNode := resolved[0]
				qt.Assert(t, qt.Equals(letNode.Name(), ""))
				letDecl := t.soleDecl(letNode, eval.DeclAlias)
				t.checkValue(letDecl, "x")
				qt.Assert(t, qt.IsTrue(slices.Contains(letNode.Expand(), x)))

				// The let's node can also be found from the root without
				// going via a use: walk the file decl's syntax to the let
				// clause, and resolve the let's own declaration
				// ident. Nodes are canonical, so this is the very same
				// node that the use resolved to.
				fileDecl := t.soleDecl(t.root(), eval.DeclFile)
				letClause := findNode(t, fileDecl.Value(), func(*ast.LetClause) bool { return true })
				t.checkResolve(fileDecl, letClause.Ident, letNode)

				// A use of a field alias resolves straight to the
				// aliased field.
				rDecl := t.soleDecl(t.field("r"), eval.DeclField)
				rSel := rDecl.Value().(*ast.SelectorExpr)
				t.checkResolve(rDecl, rSel.X, t.field("n"))
				t.checkResolve(rDecl, rSel, t.field("n.q"))

				// An old-style pattern key alias is an anonymous
				// DeclAlias binding whose value is the key's pattern
				// expression.
				kavField := findNode(t, fileDecl.Value(),
					func(f *ast.Field) bool { return renderKeyNode(f.Label) == "kav @ a.cue:8" })
				resolved = fileDecl.Resolve(kavField.Value)
				qt.Assert(t, qt.HasLen(resolved, 1))
				kaDecl := t.soleDecl(resolved[0], eval.DeclAlias)
				t.checkKey(kaDecl, "ka @ a.cue:8")
				t.checkValue(kaDecl, "string")

				// The alias and let bindings are lexical only: they are
				// not fields of the file decl, whereas the aliased
				// field n is.
				t.checkDeclFields(fileDecl, "m", "n", "r", "x")
			},
		},

		{
			// The aliasv2 experiment's postfix aliases go through the
			// same machinery as the old-style prefix aliases, and get
			// the same DeclKinds.
			name: "ResolveAliasesV2",
			archive: `-- a.cue --
@experiment(aliasv2)
package p

n~V: {q: 1}
r: V.q
a: [string]~(K,W): {name: K}
a: b: _
s: {t: self.u, u: 3}
`,
			check: func(t *graphTest) {
				// A postfix value alias resolves to the aliased field
				// itself, exactly as an old-style value alias does.
				rDecl := t.soleDecl(t.field("r"), eval.DeclField)
				rSel := rDecl.Value().(*ast.SelectorExpr)
				t.checkResolve(rDecl, rSel.X, t.field("n"))
				t.checkResolve(rDecl, rSel, t.field("n.q"))

				// A postfix key alias is an anonymous DeclAlias
				// binding, as with old-style key aliases.
				fileDecl := t.soleDecl(t.root(), eval.DeclFile)
				nameField := findNode(t, fileDecl.Value(),
					func(f *ast.Field) bool { return renderKeyNode(f.Label) == "name @ a.cue:6" })
				resolved := fileDecl.Resolve(nameField.Value)
				qt.Assert(t, qt.HasLen(resolved, 1))
				kDecl := t.soleDecl(resolved[0], eval.DeclAlias)
				t.checkKey(kDecl, "K @ a.cue:6")

				// self resolves to the node of the enclosing struct;
				// it is a keyword, not a declaration, so there is no
				// Decl for it.
				sDecl := t.soleDecl(t.field("s"), eval.DeclField)
				selfIdent := findNode(t, sDecl.Value(),
					func(id *ast.Ident) bool { return id.Name == "self" })
				t.checkResolve(sDecl, selfIdent, t.field("s"))
				selfSel := findNode(t, sDecl.Value(),
					func(*ast.SelectorExpr) bool { return true })
				t.checkResolve(sDecl, selfSel, t.field("s.u"))
			},
		},

		{
			name: "ListElements",
			archive: `-- a.cue --
package p

l: [7]
l: [8, 9]
`,
			check: func(t *graphTest) {
				// Elements are merged positionally across declarations,
				// and are not fields.
				l := t.field("l")
				t.checkFields(l)
				elements := slices.Collect(l.ListElements())
				qt.Assert(t, qt.HasLen(elements, 2))
				el0, el1 := elements[0], elements[1]

				// Element nodes are anonymous and pathless, but know
				// their index.
				qt.Assert(t, qt.Equals(el0.Name(), ""))
				i, isElement := el0.Index()
				qt.Assert(t, qt.IsTrue(isElement))
				qt.Assert(t, qt.Equals(i, 0))
				_, isElement = l.Index()
				qt.Assert(t, qt.IsFalse(isElement))
				_, ok := el0.FieldPath()
				qt.Assert(t, qt.IsFalse(ok))

				// The first element aggregates the 7 and the 8; the
				// second is just the 9.
				el0Decls := slices.Collect(el0.Decls())
				qt.Assert(t, qt.HasLen(el0Decls, 2))
				t.checkValue(el0Decls[0], "7")
				t.checkValue(el0Decls[1], "8")
				el1Decls := slices.Collect(el1.Decls())
				qt.Assert(t, qt.HasLen(el1Decls, 1))
				t.checkValue(el1Decls[0], "9")
			},
		},

		{
			name: "ListComprehensions",
			archive: `-- a.cue --
package p

l: [1, for x in [2, 3] {x}, 4]
m: [for x in [1] {a: x}]
`,
			check: func(t *graphTest) {
				// Indices are syntactic: a comprehension occupies a
				// single index no matter how many elements it would
				// yield at runtime, and later elements are not shifted
				// (the 4 is at index 2, not 3).
				l := t.field("l")
				elements := slices.Collect(l.ListElements())
				qt.Assert(t, qt.HasLen(elements, 3))
				for i, want := range []string{"1", "", "4"} {
					index, isElement := elements[i].Index()
					qt.Assert(t, qt.IsTrue(isElement))
					qt.Assert(t, qt.Equals(index, i))
					if want != "" {
						t.checkValue(t.soleDecl(elements[i], eval.DeclField), want)
					}
				}

				// The comprehension element's DeclField holds the whole
				// comprehension; its body is the DeclComprehension, and
				// the embedded x within the body is a further decl.
				compEl := elements[1]
				t.checkDeclKinds(compEl,
					eval.DeclField, eval.DeclComprehension, eval.DeclEmbedding)
				t.checkValue(t.soleDecl(compEl, eval.DeclField), "for x in [2, 3] {x}")
				bodyDecl := t.soleDecl(compEl, eval.DeclComprehension)
				t.checkValue(bodyDecl, "{x}")

				// Walking the body and resolving the embedded x reaches
				// the for clause's binding of x.
				xIdent := findNode(t, bodyDecl.Value(),
					func(id *ast.Ident) bool { return id.Name == "x" })
				resolved := bodyDecl.Resolve(xIdent)
				qt.Assert(t, qt.HasLen(resolved, 1))
				xBinding := t.soleDecl(resolved[0], eval.DeclAlias)
				t.checkKey(xBinding, "x @ a.cue:3")

				// Fields declared by a comprehension's body are fields
				// of the element node.
				mElements := slices.Collect(t.field("m").ListElements())
				qt.Assert(t, qt.HasLen(mElements, 1))
				t.checkFields(mElements[0], "a")
				t.checkDeclFields(t.soleDecl(mElements[0], eval.DeclComprehension), "a")
			},
		},

		{
			name: "Ellipses",
			archive: `-- a.cue --
package p

m: [...int]
n: {a: 1, ...}
open: {...} | {q: 1}
`,
			check: func(t *graphTest) {
				// A list ellipsis is an anonymous node whose decl
				// retains the ast.Ellipsis as its key and the type as
				// its value.
				m := t.field("m")
				mEllipses := m.Ellipses()
				qt.Assert(t, qt.HasLen(mEllipses, 1))
				ellDecl := t.soleDecl(mEllipses[0], eval.DeclEllipsis)
				_, isEllipsisNode := ellDecl.Key().(*ast.Ellipsis)
				qt.Assert(t, qt.IsTrue(isEllipsisNode))
				t.checkValue(ellDecl, "int")
				// The ellipsis contributes no elements.
				qt.Assert(t, qt.HasLen(slices.Collect(m.ListElements()), 0))

				// A struct ellipsis has no value.
				nEllipses := t.field("n").Ellipses()
				qt.Assert(t, qt.HasLen(nEllipses, 1))
				qt.Assert(t, qt.IsNil(t.soleDecl(nEllipses[0], eval.DeclEllipsis).Value()))

				// Decl.Ellipses recovers which disjunction branch is
				// open.
				open := t.field("open")
				qt.Assert(t, qt.HasLen(open.Ellipses(), 1))
				disjuncts := t.declsOfKind(open, eval.DeclDisjunct)
				qt.Assert(t, qt.HasLen(disjuncts, 2))
				qt.Assert(t, qt.HasLen(disjuncts[0].Ellipses(), 1))
				qt.Assert(t, qt.HasLen(disjuncts[1].Ellipses(), 0))
				t.checkDeclFields(disjuncts[1], "q")
			},
		},

		{
			name: "NamesParentsPaths",
			archive: `-- a.cue --
package p

outer: middle: inner: 1
`,
			check: func(t *graphTest) {
				inner := t.field("outer.middle.inner")
				qt.Assert(t, qt.Equals(inner.Name(), "inner"))
				qt.Assert(t, qt.Equals(inner.Parent(), t.field("outer.middle")))
				qt.Assert(t, qt.Equals(t.field("outer").Parent(), t.root()))

				path, ok := inner.FieldPath()
				qt.Assert(t, qt.IsTrue(ok))
				qt.Assert(t, qt.DeepEquals(path, []string{"outer", "middle", "inner"}))
			},
		},

		{
			name: "DocComments",
			archive: `-- a.cue --
package p

// x in file a
x: 1

// outer is a struct.
outer: {
	// inner has a multi-line
	// doc comment.
	inner: 42
}

// this comment is attached to c
a: b: c: _
-- b.cue --
package p

// x in file b
x: 2
`,
			check: func(t *graphTest) {
				xDecls := slices.Collect(t.field("x").Decls())
				qt.Assert(t, qt.HasLen(xDecls, 2))
				t.checkKey(xDecls[0], "x @ a.cue:4")
				t.checkDocs(xDecls[0], "// x in file a")
				t.checkKey(xDecls[1], "x @ b.cue:4")
				t.checkDocs(xDecls[1], "// x in file b")

				outerDecl := t.soleDecl(t.field("outer"), eval.DeclField)
				t.checkDocs(outerDecl, "// outer is a struct.")
				innerDecl := t.soleDecl(t.field("outer.inner"), eval.DeclField)
				t.checkDocs(innerDecl, "// inner has a multi-line\n// doc comment.")
				t.checkValue(innerDecl, "42")

				// Comments are attached semantically: on a shorthand
				// field chain, a doc comment documents the chain's leaf
				// field c, not the interior fields a and b.
				cDecl := t.soleDecl(t.field("a.b.c"), eval.DeclField)
				t.checkDocs(cDecl, "// this comment is attached to c")
				t.checkDocs(t.soleDecl(t.field("a"), eval.DeclField), "")
				t.checkDocs(t.soleDecl(t.field("a.b"), eval.DeclField), "")
			},
		},

		{
			name: "Imports",
			archive: `-- main.cue --
package p

import "example.test/dep"

use: dep.v
`,
			deps: map[string]string{
				"example.test/dep": `-- dep.cue --
// Package dep is a dependency.
package dep

v: {w: 1}
`,
			},
			check: func(t *graphTest) {
				depEval := t.dep("example.test/dep")
				root := t.root()
				t.checkFields(root, "use")

				// The selector dep.v resolves into the remote package:
				// to the canonical node for v, whose path is relative
				// to its own package's root.
				useDecl := t.soleDecl(t.field("use"), eval.DeclField)
				sel := useDecl.Value().(*ast.SelectorExpr)
				depV := depEval.Root().Field("v")
				qt.Assert(t, qt.IsNotNil(depV))
				t.checkResolve(useDecl, sel, depV)
				qt.Assert(t, qt.Equals(depV.Evaluator(), depEval))
				path, ok := depV.FieldPath()
				qt.Assert(t, qt.IsTrue(ok))
				qt.Assert(t, qt.DeepEquals(path, []string{"v"}))

				// The ident dep resolves to the (anonymous) binding the
				// import establishes; expanding it reaches the remote
				// package root.
				resolved := useDecl.Resolve(sel.X)
				qt.Assert(t, qt.HasLen(resolved, 1))
				importNode := resolved[0]
				qt.Assert(t, qt.Equals(importNode.Name(), ""))
				importDecl := t.soleDecl(importNode, eval.DeclImport)
				_, isSpec := importDecl.Value().(*ast.ImportSpec)
				qt.Assert(t, qt.IsTrue(isSpec))
				qt.Assert(t, qt.IsTrue(slices.Contains(importNode.Expand(), depEval.Root())))
				t.checkNodes(importNode.Expand().Field("v"), depV)

				// The import spec itself resolves to the remote
				// package's clauses, which carry the package docs.
				fileDecl := t.soleDecl(root, eval.DeclFile)
				spec := findNode(t, fileDecl.Value(), func(*ast.ImportSpec) bool { return true })
				t.checkResolve(fileDecl, spec.Path, depEval.PackageClauses())
				pkgDecl := t.soleDecl(depEval.PackageClauses(), eval.DeclPackage)
				t.checkDocs(pkgDecl, "// Package dep is a dependency.")
			},
		},
	}.run(t)
}
