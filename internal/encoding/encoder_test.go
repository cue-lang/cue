package encoding

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
)

func TestEncoderValidation(t *testing.T) {
	testCases := []struct {
		name string
		in   string
	}{{
		name: "RepeatedDefinitions",
		in: `
		#hello: {
			1
		}
		
		#hello: {
			"string"
		}
		`,
	},
		{
			name: "infoRepeated",
			in: `
		info: {
			"title": "test title"
		}
		info: {
			"title": "test title 2"
		}
		`,
		}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			e, err := NewEncoder(&build.File{Interpretation: build.OpenAPI, Encoding: build.JSON}, &Config{})

			if err != nil {
				t.Fatal(err)

			}
			var r cue.Runtime
			inst, err := r.CompileFile(f)
			if err != nil {
				t.Fatal(err)
			}
			err = e.Encode(inst.Value())
			if err == nil {
				t.Fatal("Expected to break")
			}

		})
	}

}
