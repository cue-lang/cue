package definitions_test

import (
	"slices"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/lsp/definitions"
	"cuelang.org/go/internal/lsp/rangeset"
	"github.com/go-quicktest/qt"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestCompletions(t *testing.T) {
	testCasesCpl{
		{
			name: "Simple",
			archive: `-- a.cue --
x: {a: int, b: {c: int, d: int}}
y: x
`,
			expectations: map[*position][]string{
				ln(1, 1, "}"): {"c", "d"},
				ln(1, 2, "}"): {"a", "b"},
				ln(2, 1, "x"): {"a", "b"},
			},
		},

		{
			name: "Explicit",
			archive: `-- a.cue --
x: a & b
b: g: int
a: f: int
y: x
`,
			expectations: map[*position][]string{
				ln(1, 1, "a"): {"f"},
				ln(1, 1, "b"): {"g"},
				ln(4, 1, "x"): {"f", "g"},
			},
		},

		{
			name: "Implicit",
			archive: `-- a.cue --
x: g: int
x: f: int
y: x
`,
			expectations: map[*position][]string{
				ln(3, 1, "x"): {"f", "g"},
			},
		},

		{
			name: "Inline_Struct_Selector",
			archive: `-- a.cue --
a: {in: {x: 5}, out: in}.out`,
			expectations: map[*position][]string{
				ln(1, 1, "}"):   {"x"},
				ln(1, 2, "in"):  {"x"},
				ln(1, 2, "}"):   {"in", "out"},
				ln(1, 2, "out"): {"x"},
			},
		},

		{
			name: "Inline_List_Index_LiteralConst",
			archive: `-- a.cue --
a: [7, {b: 3}, {c: 4, d: a}, true][2]`,
			expectations: map[*position][]string{
				ln(1, 1, "}"): {"b"},
				ln(1, 2, "a"): {"c", "d"},
				ln(1, 2, "}"): {"c", "d"},
				ln(1, 2, "]"): {"c", "d"},
			},
		},
		{
			name: "List_Index_Ellipsis_Indirect",
			archive: `-- a.cue --
x: [{a: 5}, {b: 6}, ...z]
y: x[17].a
z: a: 4`,
			expectations: map[*position][]string{
				ln(1, 1, "z"): {"a"},
				ln(1, 1, "}"): {"a"},
				ln(1, 2, "}"): {"b"},
				ln(2, 1, "]"): {"a"},
			},
		},
		{
			name: "Inline_List_Index_Dynamic",
			archive: `-- a.cue --
a: [7, {b: 3}, true][n].b
n: 1
`,
			// Even the slightest indirection defeats indexing
			expectations: map[*position][]string{
				ln(1, 1, "}"): {"b"},
			},
		},

		{
			name: "Explicit_Conjunction",
			archive: `-- a.cue --
c: {a: b, "b": x: int} & {b: x: 3, z: b.x}
b: e: 7
d: c.b.x`,
			expectations: map[*position][]string{
				ln(1, 1, "b"): {"e"},
				ln(1, 1, "}"): {"a", "b"},
				ln(1, 4, "b"): {"x"},
				ln(1, 2, "}"): {"b", "z"},
				ln(3, 1, "c"): {"a", "b", "z"},
				ln(3, 1, "b"): {"x"},
			},
		},
	}.run(t)
}

type testCaseCpl struct {
	name         string
	archive      string
	expectations map[*position][]string
}

type testCasesCpl []testCaseCpl

func (tcs testCasesCpl) run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var files []*ast.File
			filesByName := make(map[string]*ast.File)
			filesByPkg := make(map[string][]*ast.File)

			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			for _, fh := range ar.Files {
				fileAst, _ := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
				qt.Assert(t, qt.IsNotNil(fileAst))
				fileAst.Pos().File().SetContent(fh.Data)
				files = append(files, fileAst)
				filesByName[fh.Name] = fileAst
				pkgName := fileAst.PackageName()
				filesByPkg[pkgName] = append(filesByPkg[pkgName], fileAst)
			}

			for from := range tc.expectations {
				if from == self {
					continue
				}
				if from.filename == "" && len(files) == 1 {
					from.filename = files[0].Filename
				}
				from.determineOffset(filesByName[from.filename].Pos().File())
			}

			dfnsByFilename := make(map[string]*definitions.FileDefinitions)
			dfnsByPkgName := make(map[string]*definitions.Definitions)
			forPackage := func(importPath string) *definitions.Definitions {
				return dfnsByPkgName[importPath]
			}

			for pkgName, files := range filesByPkg {
				dfns := definitions.Analyse(forPackage, files...)
				dfnsByPkgName[pkgName] = dfns
				for _, fileAst := range files {
					dfnsByFilename[fileAst.Filename] = dfns.ForFile(fileAst.Filename)
				}
			}

			ranges := rangeset.NewFilenameRangeSet()

			for posFrom, completionsWant := range tc.expectations {
				slices.Sort(completionsWant)
				filename := posFrom.filename
				fdfns := dfnsByFilename[filename]
				qt.Assert(t, qt.IsNotNil(fdfns))

				offset := posFrom.offset
				ranges.Add(filename, offset, offset+len(posFrom.str))

				completionsGot := fdfns.CompletionsForOffset(offset)
				qt.Assert(t, qt.DeepEquals(completionsGot, completionsWant), qt.Commentf("from %#v", posFrom))
			}

			// Test that all offsets not explicitly mentioned in
			// expectations, complete to nothing.
			for _, fileAst := range files {
				filename := fileAst.Filename
				fdfns := dfnsByFilename[filename]
				for i := range fileAst.Pos().File().Content() {
					if ranges.Contains(filename, i) {
						continue
					}
					completionsGot := fdfns.CompletionsForOffset(i)
					qt.Check(t, qt.DeepEquals(completionsGot, nil), qt.Commentf("file: %q, offset: %d", filename, i))
				}
			}
		})
	}
}
