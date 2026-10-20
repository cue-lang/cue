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

package workspace

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
)

// TestFunctions checks definitions, references, rename and hover at
// positions within experimental function literals and their calls.
func TestFunctions(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "example.com/fn"
language: version: "v0.18.0"
-- a/a.cue --
@experiment(functions)

package a

factor: 2

scale: func(x: int, by: int = factor) -> int: x * by

y: scale(x: 3)
-- b/b.cue --
@experiment(functions)

package b

by: 0
scale: func(x: int, by~b: int = 2) -> int: x * b
y: scale(x: 3, by: 4)
-- c/c.cue --
@experiment(functions)

package c

// f returns a.
f: func(
	_~v: string = "",
	// a is returned.
	a: int,
) -> int: a
r: f("x", a: 1)
-- d/d.cue --
@experiment(functions)

package d

#Port: int
f: func(p: #Port) -> int: p
-- e/e.cue --
@experiment(functions)

package e

import "strings"

n: strings.ByteAt(b: "abc", i: 0)
`

	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		for _, filename := range []string{"a/a.cue", "b/b.cue", "c/c.cue", "d/d.cue", "e/e.cue"} {
			env.OpenFile(filename)
		}
		env.Await(env.DoneWithOpen())

		mappers := makeMappers(env, files)
		at := func(p position) protocol.Location {
			p.determinePos(mappers)
			return protocol.Location{URI: p.mapper.URI, Range: protocol.Range{Start: p.pos}}
		}
		// where renders each location as file:line:column, both
		// 1-based, in sorted order.
		where := func(locs []protocol.Location) []string {
			strs := []string{}
			for _, loc := range locs {
				filename := strings.TrimPrefix(string(loc.URI), string(rootURI)+"/")
				strs = append(strs, fmt.Sprintf("%s:%d:%d", filename, loc.Range.Start.Line+1, loc.Range.Start.Character+1))
			}
			slices.Sort(strs)
			return strs
		}

		definitions := map[position][]string{
			// The body's by.
			fln("a/a.cue", 7, 2, "by"): {},
			// The reference to factor in by's default.
			fln("a/a.cue", 7, 1, "factor"): {},
			// The call label x.
			fln("a/a.cue", 9, 1, "x"): {},
			// The call label b of a standard library function.
			fln("e/e.cue", 7, 1, "b"): {},
		}
		for p, want := range definitions {
			qt.Check(t, qt.DeepEquals(where(env.Definition(at(p))), want), qt.Commentf("definition at %v", p))
		}

		references := map[position][]string{
			// The parameter x.
			fln("a/a.cue", 7, 1, "x"): {},
			// The field factor.
			fln("a/a.cue", 5, 1, "factor"): {"a/a.cue:5:1"},
		}
		for p, want := range references {
			qt.Check(t, qt.DeepEquals(where(env.References(at(p))), want), qt.Commentf("references at %v", p))
		}

		// renames reports the edits a rename at p would make, as
		// file:line:column=text, without applying them.
		renames := func(p position, newName string) []string {
			loc := at(p)
			edit, err := env.Editor.Server.Rename(env.Ctx, &protocol.RenameParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: loc.URI},
				Position:     loc.Range.Start,
				NewName:      newName,
			})
			qt.Assert(t, qt.IsNil(err))
			strs := []string{}
			if edit == nil {
				return strs
			}
			for _, change := range edit.DocumentChanges {
				if change.TextDocumentEdit == nil {
					continue
				}
				uri := change.TextDocumentEdit.TextDocument.URI
				for _, e := range protocol.AsTextEdits(change.TextDocumentEdit.Edits) {
					strs = append(strs, fmt.Sprintf("%s=%s", where([]protocol.Location{{URI: uri, Range: e.Range}})[0], e.NewText))
				}
			}
			slices.Sort(strs)
			return strs
		}
		// The body's b is the third b on its line, after the two in
		// the parameter's by~b.
		bodyB := fln("b/b.cue", 6, 3, "b")
		bodyB.determinePos(mappers)
		qt.Assert(t, qt.Equals(string(bodyB.mapper.Content[bodyB.offset-2:bodyB.offset+1]), "* b"))

		type rename struct {
			from    position
			newName string
		}
		renameCases := map[rename][]string{
			// The field factor.
			{fln("a/a.cue", 5, 1, "factor"), "k"}: {"a/a.cue:5:1=k"},
			// The body's b, which aliases the parameter labeled by.
			{bodyB, "k"}: {},
			// The reference to #Port in p's constraint.
			{fln("d/d.cue", 6, 1, "#Port"), "#Number"}: {},
		}
		for r, want := range renameCases {
			qt.Check(t, qt.DeepEquals(renames(r.from, r.newName), want), qt.Commentf("rename at %v", r.from))
		}

		hovers := map[position]string{
			// The call label x.
			fln("a/a.cue", 9, 1, "x"): "",
			// The call label b of a standard library function.
			fln("e/e.cue", 7, 1, "b"): "",
			// The documented parameter a, at its declaration.
			fln("c/c.cue", 9, 1, "a"): "",
		}
		for p, want := range hovers {
			got, _ := env.Hover(at(p))
			gotValue := ""
			if got != nil {
				gotValue = got.Value
			}
			qt.Check(t, qt.Equals(gotValue, want), qt.Commentf("hover at %v", p))
		}
	})
}
