package buildattr

import (
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"

	"github.com/go-quicktest/qt"
)

var shouldBuildFileTests = []struct {
	testName     string
	syntax       string
	tags         map[string]bool
	wantOK       bool
	wantAttr     string
	wantError    string
	wantTagCalls map[string]bool
}{{
	testName: "EmptyFile",
	syntax:   "",
	wantOK:   true,
}, {
	testName: "PackageWithIf",
	syntax: `
@if(foo)

package something
`,
	wantOK:       false,
	wantTagCalls: map[string]bool{"foo": true},
	wantAttr:     "@if(foo)",
}, {
	testName: "PackageWithIfSuccess",
	syntax: `
@if(foo)

package something
`,
	tags:         map[string]bool{"foo": true},
	wantOK:       true,
	wantTagCalls: map[string]bool{"foo": true},
	wantAttr:     "@if(foo)",
}, {
	testName: "PackageWithIfAfterPackageClause",
	syntax: `
package something

@if(foo)
`,
	wantOK: true,
}, {
	testName: "InvalidExpr",
	syntax: `
@if(foo + bar)

package something
`,
	wantOK:   false,
	wantAttr: "@if(foo + bar)",
	wantError: `invalid operator \+ in build attribute
`,
}, {
	testName: "MultipleIfAttributes",
	syntax: `

@if(foo)
@if(bar)

package something
`,
	wantOK:   false,
	wantAttr: "@if(foo)",
	wantError: `previous declaration here:
    testfile.cue:3:1
multiple @if attributes:
    testfile.cue:4:1
`,
}, {
	testName: "MultipleIfAttributesWithOneAfterPackage",
	syntax: `

@if(foo)

package something

@if(bar)
`,
	wantOK:       false,
	wantAttr:     "@if(foo)",
	wantTagCalls: map[string]bool{"foo": true},
}, {
	testName: "And#0",
	syntax: `
@if(foo && bar)

package something
`,
	wantOK:   false,
	wantAttr: "@if(foo && bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "And#1",
	syntax: `
@if(foo && bar)

package something
`,
	tags:     map[string]bool{"foo": true},
	wantOK:   false,
	wantAttr: "@if(foo && bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "And#2",
	syntax: `
@if(foo && bar)

package something
`,
	tags:     map[string]bool{"bar": true},
	wantOK:   false,
	wantAttr: "@if(foo && bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "And#3",
	syntax: `
@if(foo && bar)

package something
`,
	tags:     map[string]bool{"foo": true, "bar": true},
	wantOK:   true,
	wantAttr: "@if(foo && bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "Or#0",
	syntax: `
@if(foo || bar)

package something
`,
	wantOK:   false,
	wantAttr: "@if(foo || bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "Or#1",
	syntax: `
@if(foo || bar)

package something
`,
	tags:     map[string]bool{"foo": true},
	wantOK:   true,
	wantAttr: "@if(foo || bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "Or#2",
	syntax: `
@if(foo || bar)

package something
`,
	tags:     map[string]bool{"bar": true},
	wantOK:   true,
	wantAttr: "@if(foo || bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "Or#3",
	syntax: `
@if(foo || bar)

package something
`,
	tags:     map[string]bool{"foo": true, "bar": true},
	wantOK:   true,
	wantAttr: "@if(foo || bar)",
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
	},
}, {
	testName: "Not#0",
	syntax: `
@if(!foo)

package something
`,
	wantOK:   true,
	wantAttr: "@if(!foo)",
	wantTagCalls: map[string]bool{
		"foo": true,
	},
}, {
	testName: "Not#1",
	syntax: `
@if(!foo)

package something
`,
	tags:     map[string]bool{"foo": true},
	wantOK:   false,
	wantAttr: "@if(!foo)",
	wantTagCalls: map[string]bool{
		"foo": true,
	},
}, {
	testName: "ComplexExpr",
	syntax: `
@if(foo || (!bar && baz))

package something
`,
	tags: map[string]bool{
		"baz": true,
	},
	wantOK: true,
	wantTagCalls: map[string]bool{
		"foo": true,
		"bar": true,
		"baz": true,
	},
	wantAttr: "@if(foo || (!bar && baz))",
}}

func TestShouldBuildFile(t *testing.T) {
	for _, test := range shouldBuildFileTests {
		t.Run(test.testName, func(t *testing.T) {
			f, err := parser.ParseFile("testfile.cue", test.syntax)
			qt.Assert(t, qt.IsNil(err))
			tagsUsed := make(map[string]bool)
			ok, attr, err := ShouldBuildFile(f, func(tag string) bool {
				tagsUsed[tag] = true
				return test.tags[tag]
			})
			qt.Check(t, qt.Equals(ok, test.wantOK))
			if test.wantAttr == "" {
				qt.Assert(t, qt.IsNil(attr))
			} else {
				qt.Assert(t, qt.Not(qt.IsNil(attr)))
				attrStr, err := format.Node(attr)
				qt.Assert(t, qt.IsNil(err))
				qt.Assert(t, qt.Equals(string(attrStr), test.wantAttr))
			}
			if test.wantError != "" {
				qt.Assert(t, qt.Not(qt.IsNil(err)))
				qt.Assert(t, qt.Matches(errors.Details(err, nil), test.wantError))
				qt.Assert(t, qt.Equals(ok, false))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			if len(tagsUsed) == 0 {
				tagsUsed = nil
			}
			qt.Check(t, qt.DeepEquals(tagsUsed, test.wantTagCalls))
		})
	}
}
