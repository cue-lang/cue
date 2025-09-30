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

package koala_test

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/xml/koala"
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
		expectedError: "test.text.$$: conflicting values int and \"content\" (mismatched types int and string):\n    input.xml:10:10\n    schema.cue:14:8\n",
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
		expectedError: "test.$v: conflicting values int and \"v2.1\" (mismatched types int and string):\n    input.xml:2:3\n    schema.cue:2:7\n",
	},
		{
			name: "Attribute Constraint Error on self-closing element",
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
			$n: int
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
			expectedError: "test.edge.$n: conflicting values int and \"2.65\" (mismatched types int and string):\n    input.xml:3:4\n    schema.cue:4:8\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fileName := "input.xml"
			dec := koala.NewDecoder(fileName, strings.NewReader(test.inputXML))

			cueExpr, err := dec.Decode()

			qt.Assert(t, qt.IsNil(err))

			rootCueFile, err := astutil.ToFile(cueExpr)
			qt.Assert(t, qt.IsNil(err))

			c := cuecontext.New()
			rootCueVal := c.BuildFile(rootCueFile, cue.Filename(fileName))

			// compile some CUE into a Value
			compiledSchema := c.CompileString(test.cueConstraints, cue.Filename("schema.cue"))

			//unify the compiledSchema against the formattedConfig
			unified := compiledSchema.Unify(rootCueVal)

			actualError := ""
			if err := unified.Validate(cue.Concrete(true)); err != nil {
				actualError = errors.Details(err, nil)
			}

			qt.Assert(t, qt.Equals(actualError, test.expectedError))
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
		name: "Simple Elements",
		inputXML: `<note>
	<to>   </to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
</note>`,
		wantCUE: `note: {
	to: $$:      "   "
	from: $$:    "Jani"
	heading: $$: "Reminder"
	body: $$:    "Don't forget me this weekend!"
}
`,
	},
		{
			name: "Simple self-closing element",
			inputXML: `<note>
	<to/>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
</note>`,
			wantCUE: `note: {
	to: {}
	from: $$:    "Jani"
	heading: $$: "Reminder"
	body: $$:    "Don't forget me this weekend!"
}
`,
		},
		{
			name: "Attribute",
			inputXML: `<note alpha="abcd">
	<to>Tove</to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
</note>`,
			wantCUE: `note: {
	$alpha: "abcd"
	to: $$:      "Tove"
	from: $$:    "Jani"
	heading: $$: "Reminder"
	body: $$:    "Don't forget me this weekend!"
}
`,
		},
		{
			name: "Attribute and Element with the same name",
			inputXML: `<note alpha="abcd">
	<to>Tove</to>
	<from>Jani</from>
	<heading>Reminder</heading>
	<body>Don't forget me this weekend!</body>
	<alpha>efgh</alpha>
</note>`,
			wantCUE: `note: {
	$alpha: "abcd"
	to: $$:      "Tove"
	from: $$:    "Jani"
	heading: $$: "Reminder"
	body: $$:    "Don't forget me this weekend!"
	alpha: $$:   "efgh"
}
`,
		},
		{
			name: "Mapping for content when an attribute exists",
			inputXML: `<note alpha="abcd">
	hello
</note>`,
			wantCUE: `note: {
	$alpha: "abcd"
	$$:     "\n\thello\n"
}
`,
		},
		{
			name: "Nested Element",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
</notes>`,
			wantCUE: `notes: note: {
	$alpha: "abcd"
	$$:     "hello"
}
`,
		},
		{
			name: "Collections",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
	<note alpha="abcdef">goodbye</note>
</notes>`,
			wantCUE: `notes: note: [{
	$alpha: "abcd"
	$$:     "hello"
}, {
	$alpha: "abcdef"
	$$:     "goodbye"
}]
`,
		},
		{
			name: "Interleaving Element Types",
			inputXML: `<notes>
	<note alpha="abcd">hello</note>
	<note alpha="abcdef">goodbye</note>
	<book>mybook</book>
	<note alpha="ab">goodbye</note>
	<note>direct</note>
</notes>`,
			wantCUE: `notes: {
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
	book: $$: "mybook"
}
`,
		},
		{
			name: "Namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
  </h:tr>
</h:table>`,
			wantCUE: `"h:table": {
	"$xmlns:h": "http://www.w3.org/TR/html4/"
	"h:tr": "h:td": [{
		$$: "Apples"
	}, {
		$$: "Bananas"
	}]
}
`,
		},
		{
			name: "Attribute namespace prefix",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:f="http://www.w3.org/TR/html5/">
  <h:tr>
    <h:td f:type="fruit">Apples</h:td>
    <h:td>Bananas</h:td>
  </h:tr>
</h:table>`,
			wantCUE: `"h:table": {
	"$xmlns:h": "http://www.w3.org/TR/html4/"
	"$xmlns:f": "http://www.w3.org/TR/html5/"
	"h:tr": "h:td": [{
		"$f:type": "fruit"
		$$:        "Apples"
	}, {
		$$: "Bananas"
	}]
}
`,
		},
		{
			name: "Mixed Namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:r="d">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
    <r:blah>e3r</r:blah>
  </h:tr>
</h:table>`,
			wantCUE: `"h:table": {
	"$xmlns:h": "http://www.w3.org/TR/html4/"
	"$xmlns:r": "d"
	"h:tr": {
		"h:td": [{
			$$: "Apples"
		}, {
			$$: "Bananas"
		}]
		"r:blah": $$: "e3r"
	}
}
`,
		},
		{
			name: "Elements with same name but different namespaces",
			inputXML: `<h:table xmlns:h="http://www.w3.org/TR/html4/" xmlns:r="d">
  <h:tr>
    <h:td>Apples</h:td>
    <h:td>Bananas</h:td>
    <r:td>e3r</r:td>
  </h:tr>
</h:table>`,
			wantCUE: `"h:table": {
	"$xmlns:h": "http://www.w3.org/TR/html4/"
	"$xmlns:r": "d"
	"h:tr": {
		"h:td": [{
			$$: "Apples"
		}, {
			$$: "Bananas"
		}]
		"r:td": $$: "e3r"
	}
}
`,
		},
		{
			name: "Collection of elements, where elements have optional properties",
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
			wantCUE: `books: book: [{
	title: $$:  "title"
	author: $$: "John Doe"
}, {
	title: $$:  "title2"
	author: $$: "Jane Doe"
}, {
	title: $$:  "Lord of the rings"
	author: $$: "JRR Tolkien"
	volume: [{
		title: $$:  "Fellowship"
		author: $$: "JRR Tolkien"
	}, {
		title: $$:  "Two Towers"
		author: $$: "JRR Tolkien"
	}, {
		title: $$:  "Return of the King"
		author: $$: "JRR Tolkien"
	}]
}]
`,
		},
		{
			name:     "Carriage Return Filter Test",
			inputXML: "<node>\r\nhello\r\n</node>",
			wantCUE: `node: $$: "\nhello\n"
`,
		},
		{
			name: "Spacing either side of xml (including new lines before and after root node)",
			inputXML: `
			
			<root>
		<message>Hello World!</message>
		<nested>
			<a1>one level</a1>
			<a2>
				<b>two levels</b>
			</a2>
		</nested>
	</root>
	
	`,
			wantCUE: `root: {
	message: $$: "Hello World!"
	nested: {
		a1: $$: "one level"
		a2: b: $$: "two levels"
	}
}
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dec := koala.NewDecoder("input.xml", strings.NewReader(test.inputXML))
			cueExpr, err := dec.Decode()

			qt.Assert(t, qt.IsNil(err))

			rootCueFile, err := astutil.ToFile(cueExpr)
			qt.Assert(t, qt.IsNil(err))

			actualCue, err := format.Node(rootCueFile)

			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(string(actualCue), test.wantCUE))
		})
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		inputXML      string
		expectedError string
	}{
		{
			name: "Text after root node followed by subelements",
			inputXML: `<note>
		mixed
		<from>Jani</from>
		<heading>Reminder</heading>
		<body>Don't forget me this weekend!</body>
		</note>`,
			expectedError: `text content within an XML element that has sub-elements is not supported`,
		},
		{
			name: "Text in middle of subelements",
			inputXML: `<note>
		<to/>
		mixed
		<from>Jani</from>
		<heading>Reminder</heading>
		<body>Don't forget me this weekend!</body>
	</note>`,
			expectedError: `text content within an XML element that has sub-elements is not supported`,
		},
		{
			name: "Nested mixed content",
			inputXML: `<note>
		<to/>
		<from>Jani <subElement/></from>
		<heading>Reminder</heading>
		<body>Don't forget me this weekend!</body>
	</note>`,
			expectedError: `text content within an XML element that has sub-elements is not supported`,
		},
		{
			name: "Text before end of root element",
			inputXML: `<note>
		<to/>
		<from></from>
		<heading>Reminder</heading>
		myText
	</note>`,
			expectedError: `text content within an XML element that has sub-elements is not supported`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dec := koala.NewDecoder("input.xml", strings.NewReader(test.inputXML))
			_, err := dec.Decode()

			qt.Assert(t, qt.ErrorMatches(err, test.expectedError))
		})
	}
}
