// Copyright 2025 The CUE Authors
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

// Package koala converts XML to and from CUE as described here: https://github.com/cue-lang/cue/discussions/3776
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package koala

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"github.com/go-quicktest/qt"
)

func TestErrorReporting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		inputXML       string
		cueConstraints string
		expectedError  string
	}{{
		name: "Element Text Content Constraint Error",
		inputXML: `<?xml version="1.0" encoding="UTF-8"?>
  <test v="v2.1">
   <edge n="2.65" o="3.65"/>
   <container id="555"/>
   <container id="777"/>
   <container id="888" >
    <l attr="x"/>
    <l attr="y"/>
   </container>
   <text>content</text>
  </test>`,
		cueConstraints: `test: {
		$v: string
		edge: {
			$n: string
			$o: string
		}
		container: [...{
			$id: string
			l: [...{
				$attr: string
			}]
		}]
		text: {
			$$: int
		}
	}`,
		expectedError: `myXmlFile.xml:10:10
schema.cue:14:8
test.text.$$: conflicting values int and "content" (mismatched types int and string)
`,
	}, {
		name: "Attribute Constraint Error",
		inputXML: `<?xml version="1.0" encoding="UTF-8"?>
  <test v="v2.1">
   <edge n="2.65" o="3.65"/>
   <container id="555"/>
   <container id="777"/>
   <container id="888" >
    <l attr="x"/>
    <l attr="y"/>
   </container>
   <text>content</text>
  </test>`,
		cueConstraints: `test: {
		$v: int
		edge: {
			$n: string
			$o: string
		}
		container: [...{
			$id: string
			l: [...{
				$attr: string
			}]
		}]
		text: {
			$$: string
		}
	}`,
		expectedError: `myXmlFile.xml:2:11
schema.cue:2:7
test.$v: conflicting values int and "v2.1" (mismatched types int and string)
`,
	},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var err error
			fileName := "myXmlFile.xml"
			dec := NewDecoder(fileName, strings.NewReader(test.inputXML))
			cueExpr, err := dec.Decode()

			qt.Assert(t, qt.IsNil(err))

			rootCueFile, _ := astutil.ToFile(cueExpr)
			c := cuecontext.New()
			rootCueVal := c.BuildFile(rootCueFile, cue.Filename(fileName))

			// compile some CUE into a Value
			compiledSchema := c.CompileString(test.cueConstraints, cue.Filename("schema.cue"))

			//unify the compiledSchema against the formattedConfig
			unified := compiledSchema.Unify(rootCueVal)

			actualError := ""
			if err := unified.Validate(cue.Concrete(true), cue.Schema()); err != nil {

				for _, e := range errors.Errors(err) {

					positions := errors.Positions(e)
					for _, p := range positions {
						actualError += fmt.Sprintf("%s\n", p)
					}
					actualError += fmt.Sprintf("%s\n", e.Error())
				}
			}

			qt.Assert(t, qt.Equals(actualError, test.expectedError))
			qt.Assert(t, qt.IsNil(err))
		})
	}
}

func TestElementDecoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		inputXML string
		wantCUE  string
	}{{
		name: "1. Simple Elements",
		inputXML: `<note>
	<to>   </to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
</note>`,
		wantCUE: `{
	note: {
		to: {
			$$: "   "
		}
		from: {
			$$: "Jani"
		}
		heading: {
			$$: "Reminder"
		}
		body: {
			$$: "Don't forget me this weekend!"
		}
	}
}`,
	},
		{
			name: "2. Attribute",
			inputXML: `<note alpha="abcd">
	<to>Tove</to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
</note>`,
			wantCUE: `{
	note: {
		$alpha: "abcd"
		to: {
			$$: "Tove"
		}
		from: {
			$$: "Jani"
		}
		heading: {
			$$: "Reminder"
		}
		body: {
			$$: "Don't forget me this weekend!"
		}
	}
}`,
		},
		{
			name: "3. Attribute and Element with the same name",
			inputXML: `<note alpha="abcd">
	<to>Tove</to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
	<alpha>efgh</alpha>
</note>`,
			wantCUE: `{
	note: {
		$alpha: "abcd"
		to: {
			$$: "Tove"
		}
		from: {
			$$: "Jani"
		}
		heading: {
			$$: "Reminder"
		}
		body: {
			$$: "Don't forget me this weekend!"
		}
		alpha: {
			$$: "efgh"
		}
	}
}`,
		},
		{
			name: "4. Mapping for content when an attribute exists",
			inputXML: `<note alpha="abcd">
	hello
</note>`,
			wantCUE: `{
	note: {
		$alpha: "abcd"
		$$: """

			\thello

			"""
	}
}`,
		},
		{
			name: "5. Nested Element",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
</notes>`,
			wantCUE: `{
	notes: {
		note: {
			$alpha: "abcd"
			$$:     "hello"
		}
	}
}`,
		},
		{
			name: "6. Collections",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
	<note alpha="abcdef">goodbye</note>
</notes>`,
			wantCUE: `{
	notes: {
		note: [{
			$alpha: "abcd"
			$$:     "hello"
		}, {
			$alpha: "abcdef"
			$$:     "goodbye"
		}]
	}
}`,
		},
		{
			name: "7. Interleaving Element Types",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
	<note alpha="abcdef">goodbye</note>
	<book>mybook</book>
	<note alpha="ab">goodbye</note>
	<note>direct</note>
</notes>`,
			wantCUE: `{
	notes: {
		note: [{
			$alpha: "abcd"
			$$:     "hello"
		}, {
			$alpha: "abcdef"
			$$:     "goodbye"
		}, {
			$alpha: "ab"
			$$:     "goodbye"
		}, {
			$$: "direct"
		}]
		book: {
			$$: "mybook"
		}
	}
}`,
		},
		{
			name: "8. Namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
  </h:tr>
</h:table>`,
			wantCUE: `{
	"h:table": {
		"$xmlns:h": "http://www.w3.org/TR/html4/"
		"h:tr": {
			"h:td": [{
				$$: "Apples"
			}, {
				$$: "Bananas"
			}]
		}
	}
}`,
		},
		{
			name: "8.1. Attribute namespace prefix",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:f="http://www.w3.org/TR/html5/">
  <h:tr>
    <h:td f:type="fruit">Apples</h:td>
    <h:td>Bananas</h:td>
  </h:tr>
</h:table>`,
			wantCUE: `{
	"h:table": {
		"$xmlns:h": "http://www.w3.org/TR/html4/"
		"$xmlns:f": "http://www.w3.org/TR/html5/"
		"h:tr": {
			"h:td": [{
				"$f:type": "fruit"
				$$:        "Apples"
			}, {
				$$: "Bananas"
			}]
		}
	}
}`,
		},
		{
			name: "9. Mixed Namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:r="d">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
    <r:blah>e3r</r:blah>
  </h:tr>
</h:table>`,
			wantCUE: `{
	"h:table": {
		"$xmlns:h": "http://www.w3.org/TR/html4/"
		"$xmlns:r": "d"
		"h:tr": {
			"h:td": [{
				$$: "Apples"
			}, {
				$$: "Bananas"
			}]
			"r:blah": {
				$$: "e3r"
			}
		}
	}
}`,
		},
		{
			name: "10. Elements with same name but different namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:r="d">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
    <r:td>e3r</r:td>
  </h:tr>
</h:table>`,
			wantCUE: `{
	"h:table": {
		"$xmlns:h": "http://www.w3.org/TR/html4/"
		"$xmlns:r": "d"
		"h:tr": {
			"h:td": [{
				$$: "Apples"
			}, {
				$$: "Bananas"
			}]
			"r:td": {
				$$: "e3r"
			}
		}
	}
}`,
		},
		{
			name: "11. Collection of elements, where elements have optional properties",
			inputXML: `<books>
    <book>
        <title>title</title>
        <author>John Doe</author>
    </book>
    <book>
        <title>title2</title>
        <author>Jane Doe</author>
    </book>
    <book>
        <title>Lord of the rings</title>
        <author>JRR Tolkien</author>
        <volume>
            <title>Fellowship</title>
            <author>JRR Tolkien</author>
        </volume>
        <volume>
            <title>Two Towers</title>
            <author>JRR Tolkien</author>
        </volume>
        <volume>
            <title>Return of the King</title>
            <author>JRR Tolkien</author>
        </volume>
    </book>
</books>`,
			wantCUE: `{
	books: {
		book: [{
			title: {
				$$: "title"
			}
			author: {
				$$: "John Doe"
			}
		}, {
			title: {
				$$: "title2"
			}
			author: {
				$$: "Jane Doe"
			}
		}, {
			title: {
				$$: "Lord of the rings"
			}
			author: {
				$$: "JRR Tolkien"
			}
			volume: [{
				title: {
					$$: "Fellowship"
				}
				author: {
					$$: "JRR Tolkien"
				}
			}, {
				title: {
					$$: "Two Towers"
				}
				author: {
					$$: "JRR Tolkien"
				}
			}, {
				title: {
					$$: "Return of the King"
				}
				author: {
					$$: "JRR Tolkien"
				}
			}]
		}]
	}
}`,
		},
		{
			name:     "12. Carriage Return Filter Test",
			inputXML: "<node>\r\nhello\r\n</node>",
			wantCUE: `{
	node: {
		$$: """

			hello

			"""
	}
}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fileName := "myXmlFile.xml"

			dec := NewDecoder(fileName, strings.NewReader(test.inputXML))
			cueExpr, err := dec.Decode()

			qt.Assert(t, qt.IsNil(err))

			rootCueFile, _ := astutil.ToFile(cueExpr)
			c := cuecontext.New()
			rootCueVal := c.BuildFile(rootCueFile, cue.Filename(fileName))

			actualCue := fmt.Sprintf("%v", rootCueVal)

			qt.Assert(t, qt.Equals(actualCue, test.wantCUE))
			qt.Assert(t, qt.IsNil(err))
		})
	}
}
