// Copyright 2018 The CUE Authors
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

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"github.com/spf13/cobra"
)

// TODO:
// - remove the limitations mentioned in the documentation
// - implement verification post-processing as extra safety

// newTrimCmd creates a trim command
func newTrimCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trim",
		Short: "remove superfluous fields",
		Long: `trim removes fields from structs that are already defined by a template

A field, struct, or list is removed if it is implied by a template, a list type
value, a comprehension or any other implied content. It will modify the files
in place.

Limitations
Removal is on a best effort basis. Some caveats:
- Fields in implied content may refer to fields within the struct in which
  they are included, but are only resolved on a best-effort basis.
- Disjunctions that contain structs in implied content cannot be used to
  remove fields.
- There is currently no verification step: manual verification is required.

Examples:

	$ cat <<EOF > foo.cue
	light <Name>: {
		room:          string
		brightnessOff: 0.0 | >=0 & <=100.0
		brightnessOn:  100.0 | >=0 & <=100.0
	}

	light ceiling50: {
		room:         "MasterBedroom"
		brightnessOn:  100 // this line
		brightnessOff: 0   // and this line will be removed
	}
	EOF

	$ cue trim foo.cue
	$ cat foo.cue
	light <Name>: {
		room:          string
		brightnessOff: 0.0 | >=0 & <=100.0
		brightnessOn:  100.0 | >=0 & <=100.0
	}

	light ceiling50: {
		room: "MasterBedroom"
	}

It is guaranteed that the resulting files give the same output as before the
removal.
`,
		RunE: runTrim,
	}

	flagOut.Add(cmd)
	return cmd
}

func runTrim(cmd *cobra.Command, args []string) error {
	// TODO: Do something more fine-grained. Optional fields are mostly not
	// useful to consider as an optional field will almost never subsume
	// another value. However, an optional field may subsume and therefore
	// trigger the removal of another optional field.
	// For now this is the better approach: trimming is not 100% accurate,
	// and optional fields are just more likely to cause edge cases that may
	// block a removal.
	internal.DropOptional = true
	defer func() { internal.DropOptional = false }()

	log.SetOutput(cmd.OutOrStderr())

	ctxt := build.NewContext(build.ParseOptions(parser.ParseComments))
	binst := load.Instances(args, &load.Config{Context: ctxt})

	instances := cue.Build(binst)
	for _, inst := range instances {
		if err := inst.Err; err != nil {
			return err
		}
	}

	// dst := flagName("o").String(cmd)
	dst := flagOut.String(cmd)
	if dst != "" && dst != "-" {
		switch _, err := os.Stat(dst); {
		case os.IsNotExist(err):
		case err == nil:
		default:
			return fmt.Errorf("error creating file: %v", err)
		}
	}

	for i, inst := range binst {
		gen := newTrimSet(cmd)
		for _, f := range inst.Files {
			gen.markNodes(f)
		}

		root := instances[i].Lookup()
		rm := gen.trim("root", root, cue.Value{}, root)

		if flagDryrun.Bool(cmd) {
			continue
		}

		for _, f := range inst.Files {
			filename := f.Filename

			f.Decls = gen.trimDecls(f.Decls, rm, root, true)

			opts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				opts = append(opts, format.Simplify())
			}

			var buf bytes.Buffer
			err := format.Node(&buf, f, opts...)
			if err != nil {
				return fmt.Errorf("error formatting file: %v", err)
			}

			if dst == "-" {
				_, err := io.Copy(cmd.OutOrStdout(), &buf)
				if err != nil {
					return err
				}
				continue
			} else if dst != "" {
				filename = dst
			}

			err = ioutil.WriteFile(filename, buf.Bytes(), 0644)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type trimSet struct {
	cmd       *cobra.Command
	stack     []string
	exclude   map[ast.Node]bool // don't remove fields marked here
	alwaysGen map[ast.Node]bool // node is always from a generated source
	fromComp  map[ast.Node]bool // node originated from a comprehension
}

func newTrimSet(cmd *cobra.Command) *trimSet {
	return &trimSet{
		cmd:       cmd,
		exclude:   map[ast.Node]bool{},
		alwaysGen: map[ast.Node]bool{},
		fromComp:  map[ast.Node]bool{},
	}
}

func (t *trimSet) path() string {
	return strings.Join(t.stack[1:], " ")
}

func (t *trimSet) traceMsg(msg string) {
	if flagTrace.Bool(t.cmd) {
		fmt.Print(t.path())
		msg = strings.TrimRight(msg, "\n")
		msg = strings.Replace(msg, "\n", "\n    ", -1)
		fmt.Printf(": %s\n", msg)
	}
}

func (t *trimSet) markNodes(n ast.Node) {
	ast.Walk(n, nil, func(n ast.Node) {
		switch x := n.(type) {
		case *ast.Ident:
			if x.Node != nil {
				t.exclude[x.Node] = true
			}
			if x.Scope != nil {
				t.exclude[x.Scope] = true
			}

		case *ast.ListLit:
			if x.Type != nil {
				t.markAlwaysGen(x.Type, false)
			}

		case *ast.Field:
			if _, ok := x.Label.(*ast.TemplateLabel); ok {
				t.markAlwaysGen(x.Value, false)
			}

		case *ast.ListComprehension, *ast.ComprehensionDecl:
			t.markAlwaysGen(x, true)
		}
	})
}

func (t *trimSet) markAlwaysGen(first ast.Node, isComp bool) {
	ast.Walk(first, func(n ast.Node) bool {
		if t.alwaysGen[n] {
			return false
		}
		t.alwaysGen[n] = true
		if isComp {
			t.fromComp[n] = true
		}
		if x, ok := n.(*ast.Ident); ok && n != first {
			// Also mark any value used within a template.
			if x.Node != nil {
				// fmt.Println("MARKED", internal.DebugStr(x.Node),
				// "by", internal.DebugStr(first))
				// t.markAlwaysGen(x.Node)
			}
		}
		return true
	}, nil)
}

func (t *trimSet) canRemove(n ast.Node) bool {
	return !t.exclude[n] && !t.alwaysGen[n]
}

func isDisjunctionOfStruct(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.OR {
			return hasStruct(x.X) || hasStruct(x.Y)
		}
	}
	return false
}

func hasStruct(n ast.Node) bool {
	hasStruct := false
	ast.Walk(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.StructLit); ok {
			hasStruct = true
		}
		return !hasStruct
	}, nil)
	return hasStruct
}

// trim strips fields from structs that would otherwise be generated by implied
// content, such as templates, comprehensions, and list types.
//
// The algorithm walks the tree with two values in parallel: one for the full
// configuration, and one for implied content. For each node in the tree it
// determines the value of the implied content and that of the full value
// and strips any of the non-implied fields if it subsumes the implied ones.
//
// There are a few gotchas:
// - Fields in the implied content may refer to fields in the complete config.
//   To support this, incomplete fields are detected and evaluated within the
//   configuration.
// - Templates are instantiated as a result of the declaration of concrete
//   sibling fields. Such fields should not be removed even if the instantiated
//   template completely subsumes such fields as the reason to instantiate
//   the template will disappear with it.
// - As the parallel structure is different, it may resolve to different
//   default values. There is no support yet for selecting defaults of a value
//   based on another value without doing a full unification. So for now we
//   skip any disjunction containing structs.
//
//		v      the current value
//		m      the "mixed-in" values
//		scope  in which to evaluate expressions
//		rmSet  nodes in v that may be removed by the caller
func (t *trimSet) trim(label string, v, m, scope cue.Value) (rmSet []ast.Node) {
	saved := t.stack
	t.stack = append(t.stack, label)
	defer func() { t.stack = saved }()

	vSplit := v.Split()

	// At the moment disjunctions of structs are note supported. Detect them and
	// punt.
	// TODO: support disjunctions.
	mSplit := m.Split()
	for _, v := range mSplit {
		if isDisjunctionOfStruct(v.Source()) {
			return
		}
	}

	// Collect generated nodes.
	// Only keep the good parts of the template.
	// Incoming structs may be incomplete resulting in errors. It is safe
	// to ignore these. If there is an actual error, it will manifest in
	// the evaluation of v.
	in := cue.Value{}
	gen := []ast.Node{}
	for _, v := range mSplit {
		// TODO: consider resolving incomplete values within the current
		// scope, as we do for fields.
		if v.IsValid() {
			in = in.Unify(v)
		}
		gen = append(gen, v.Source())
	}

	switch v.Kind() {
	case cue.StructKind:
		// TODO: merge template preprocessing with that of fields.

		// Identify generated components and unify them with the mixin value.
		exists := false
		for _, v := range v.Split() {
			if src := v.Source(); t.alwaysGen[src] {
				if w := in.Unify(v); w.Err() == nil {
					in = w
				}
				// One of the sources of this struct is generated. That means
				// we can safely delete a non-generated version.
				exists = true
				gen = append(gen, src)
			}
		}

		// Build map of mixin fields.
		valueMap := map[key]cue.Value{}
		for mIter, _ := in.Fields(cue.All(), cue.Optional(false)); mIter.Next(); {
			valueMap[iterKey(mIter)] = mIter.Value()
		}

		fn := v.Template()

		// Process fields.
		rm := []ast.Node{}
		for iter, _ := v.Fields(cue.All()); iter.Next(); {
			mSub := valueMap[iterKey(iter)]
			if fn != nil {
				mSub = mSub.Unify(fn(iter.Label()))
			}

			removed := t.trim(iter.Label(), iter.Value(), mSub, v)
			rm = append(rm, removed...)
		}

		canRemove := fn == nil
		for _, v := range vSplit {
			src := v.Source()
			if t.fromComp[src] {
				canRemove = true
			}
		}

		// Remove fields from source.
		for _, v := range vSplit {
			if src := v.Source(); !t.alwaysGen[src] {
				switch x := src.(type) {
				case *ast.File:
					// TODO: use in instead?
					x.Decls = t.trimDecls(x.Decls, rm, m, canRemove)

				case *ast.StructLit:
					x.Elts = t.trimDecls(x.Elts, rm, m, canRemove)
					exists = exists || m.Exists()
					if len(x.Elts) == 0 && exists && t.canRemove(src) && !inNodes(gen, src) {
						rmSet = append(rmSet, src)
					}

				default:
					if len(t.stack) == 1 {
						// TODO: fix this hack to pass down the fields to remove
						return rm
					}
				}
			}
		}

		if flagTrace.Bool(t.cmd) {
			w := &bytes.Buffer{}
			fmt.Fprintln(w)
			fmt.Fprintln(w, "value:    ", v)
			if in.Exists() {
				fmt.Fprintln(w, "mixed in: ", in)
			}
			for _, v := range vSplit {
				status := "[]"
				src := v.Source()
				if inNodes(rmSet, src) {
					status = "[re]"
				} else if t.alwaysGen[src] {
					status = "[i]"
				}
				fmt.Fprintf(w, "    %4s %v: %v %T\n", status, v.Pos(), internal.DebugStr(src), src)
			}

			t.traceMsg(w.String())
		}

	case cue.ListKind:
		mIter, _ := m.List()
		i := 0
		rmElem := []ast.Node{}
		for iter, _ := v.List(); iter.Next(); i++ {
			mIter.Next()
			rm := t.trim(strconv.Itoa(i), iter.Value(), mIter.Value(), scope)
			rmElem = append(rmElem, rm...)
		}

		// Signal the removal of lists of which all elements have been marked
		// for removal.
		for _, v := range vSplit {
			if src := v.Source(); !t.alwaysGen[src] {
				l, ok := src.(*ast.ListLit)
				if !ok {
					break
				}
				rmList := true
				iter, _ := v.List()
				for i := 0; i < len(l.Elts) && iter.Next(); i++ {
					if !inNodes(rmElem, l.Elts[i]) {
						rmList = false
						break
					}
				}
				if rmList && m.Exists() && t.canRemove(src) && !inNodes(gen, src) {
					rmSet = append(rmSet, src)
				}
			}
		}
		fallthrough

	default:
		for _, v := range vSplit {
			src := v.Source()
			if t.alwaysGen[src] || inNodes(gen, src) {
				if v.IsIncomplete() {
					// The template has an expression that cannot be fully
					// resolved. Attempt to complete the expression by
					// evaluting it within the struct to which the template
					// is applied.
					expr := v.Syntax()
					// TODO: this only resolves references contained in scope.
					v = internal.EvalExpr(scope, expr).(cue.Value)
				}
				in = in.Unify(v)
				gen = append(gen, src)
			}
		}

		// Mark any subsumed part that is covered by generated config.
		if in.Err() == nil && v.Subsumes(in) {
			for _, v := range vSplit {
				src := v.Source()
				if t.canRemove(src) && !inNodes(gen, src) {
					rmSet = append(rmSet, src)
				}
			}
		}

		if flagTrace.Bool(t.cmd) {
			w := &bytes.Buffer{}
			if len(rmSet) > 0 {
				fmt.Fprint(w, "field: SUBSUMED\n")
			} else {
				fmt.Fprint(w, "field: \n")
			}
			fmt.Fprintln(w, "value:    ", v)
			if in.Exists() {
				fmt.Fprintln(w, "mixed in: ", in)
			}
			for _, v := range vSplit {
				status := "["
				if inNodes(gen, v.Source()) {
					status += "i"
				}
				if inNodes(rmSet, v.Source()) {
					status += "r"
				}
				status += "]"
				src := v.Source()
				fmt.Fprintf(w, "   %4s %v: %v\n", status, v.Pos(), internal.DebugStr(src))
			}

			t.traceMsg(w.String())
		}
	}
	return rmSet
}

func (t *trimSet) trimDecls(decls []ast.Decl, rm []ast.Node, m cue.Value, allow bool) []ast.Decl {
	a := make([]ast.Decl, 0, len(decls))

	for _, d := range decls {
		if f, ok := d.(*ast.Field); ok {
			label, _ := ast.LabelName(f.Label)
			v := m.Lookup(label)
			if inNodes(rm, f.Value) && (allow || v.Exists()) {
				continue
			}
		}
		a = append(a, d)
	}
	return a
}

func inNodes(a []ast.Node, n ast.Node) bool {
	for _, e := range a {
		if e == n {
			return true
		}
	}
	return false
}

func deleteNode(a []ast.Node, n ast.Node) []ast.Node {
	k := 0
	for _, e := range a {
		if e != n {
			a[k] = e
			k++
		}
	}
	return a[:k]
}

type key struct {
	label  string
	hidden bool
}

func iterKey(v cue.Iterator) key {
	return key{v.Label(), v.IsHidden()}
}
