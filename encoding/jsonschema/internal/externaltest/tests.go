package externaltest

import (
	"bytes"
	stdjson "encoding/json"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/interpreter/embed"
	"cuelang.org/go/cue/load"
)

type Schema struct {
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Schema      stdjson.RawMessage `json:"schema"`
	Skip        string             `json:"skip,omitempty"`
	Tests       []*Test            `json:"tests"`
}

type Test struct {
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Data        stdjson.RawMessage `json:"data"`
	Valid       bool               `json:"valid"`
	Skip        string             `json:"skip,omitempty"`
}

func ReadTestDir(dir string) (tests map[string][]*Schema, err error) {
	os.Setenv("CUE_EXPERIMENT", "embed")
	inst := load.Instances([]string{"."}, &load.Config{
		Dir: dir,
	})[0]
	if err != nil {
		return nil, err
	}
	ctx := cuecontext.New(cuecontext.Interpreter(embed.New()))
	instVal := ctx.BuildInstance(inst)
	if err := instVal.Err(); err != nil {
		return nil, err
	}
	val := instVal.LookupPath(cue.MakePath(cue.Str("allTests")))
	if err := val.Err(); err != nil {
		return nil, err
	}
	if err := val.Decode(&tests); err != nil {
		return nil, err
	}
	// Fix up the raw JSON data to avoid running into some decode issues.
	for _, schemas := range tests {
		for _, schema := range schemas {
			for _, test := range schema.Tests {
				if len(test.Data) == 0 {
					// See https://github.com/cue-lang/cue/issues/3397
					test.Data = []byte("null")
					continue
				}
				// See https://github.com/cue-lang/cue/issues/3398
				test.Data = bytes.ReplaceAll(test.Data, []byte("\ufeff"), []byte(`\ufeff`))
			}
		}
	}
	return tests, nil
}
