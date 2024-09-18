package externaltest

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/interpreter/embed"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/token"
)

type Schema struct {
	location
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Schema      stdjson.RawMessage `json:"schema"`
	Skip        Skip               `json:"skip,omitempty"`
	Tests       []*Test            `json:"tests"`
}

type Test struct {
	location
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Data        stdjson.RawMessage `json:"data"`
	Valid       bool               `json:"valid"`
	Skip        Skip               `json:"skip,omitempty"`
}

// Skip records information about whether a given schema
// or test will be skipped when testing. If not present,
// the test will be expected to pass.
//
// Each key in the map represents the name of a point
// in the cuetdtest matrix.
type Skip map[string]string

type location struct {
	root cue.Value
	path cue.Path
}

func (loc location) Pos() token.Pos {
	return loc.root.LookupPath(loc.path).Pos()
}

// WriteTestDir writes test data files as read by ReadTestDir
// to the given directory. The keys of tests are filenames relative
// to dir.
func WriteTestDir(dir string, tests map[string][]*Schema) error {
	for filename, schemas := range tests {
		filename = filepath.Join(dir, filename)
		data, err := stdjson.MarshalIndent(schemas, "", "\t")
		if err != nil {
			return err
		}
		if err != nil {
			return err
		}
		data = append(data, '\n')
		oldData, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if bytes.Equal(oldData, data) {
			continue
		}
		err = os.WriteFile(filename, data, 0o666)
		if err != nil {
			return err
		}
	}
	return nil
}

var ErrNotFound = fmt.Errorf("no external JSON schema tests found")

// ReadTestDir reads all the external tests from the given directory.
func ReadTestDir(dir string) (tests map[string][]*Schema, err error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	os.Setenv("CUE_EXPERIMENT", "embed")
	inst := load.Instances([]string{"."}, &load.Config{
		Dir: dir,
		// Just like in the cue/load tests, prevent Go tests from walking up to the root
		// directory of the git repository, as that almost always causes test cache misses.
		ModuleRoot: dir,
	})[0]
	if err := inst.Err; err != nil {
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
	for filename, schemas := range tests {
		for i, schema := range schemas {
			schema.location = location{
				root: val,
				path: cue.MakePath(cue.Str(filename), cue.Index(i)),
			}
			for j, test := range schema.Tests {
				test.location = location{
					root: val,
					path: cue.MakePath(cue.Str(filename), cue.Index(i), cue.Str("tests"), cue.Index(j)),
				}
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
