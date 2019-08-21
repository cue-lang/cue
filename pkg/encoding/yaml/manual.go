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

package yaml

import (
	"bytes"
	"io"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/third_party/yaml"
	goyaml "github.com/ghodss/yaml"
)

// Marshal returns the YAML encoding of v.
func Marshal(v cue.Value) (string, error) {
	b, err := goyaml.Marshal(v)
	return string(b), err
}

// MarshalStream returns the YAML encoding of v.
func MarshalStream(v cue.Value) (string, error) {
	// TODO: return an io.Reader and allow asynchronous processing.
	iter, err := v.List()
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	for i := 0; iter.Next(); i++ {
		if i > 0 {
			buf.WriteString("---\n")
		}
		b, err := goyaml.Marshal(iter.Value())
		if err != nil {
			return "", err
		}
		buf.Write(b)
	}
	return buf.String(), nil
}

// Unmarshal parses the YAML to a CUE instance.
func Unmarshal(data []byte) (ast.Expr, error) {
	return yaml.Unmarshal("", data)
}

// Validate validates YAML and confirms it matches the constraints
// specified by v. If the YAML source is a stream, every object must match v.
func Validate(b []byte, v cue.Value) (bool, error) {
	d, err := yaml.NewDecoder("yaml.Validate", b)
	if err != nil {
		return false, err
	}
	r := internal.GetRuntime(v).(*cue.Runtime)
	for {
		expr, err := d.Decode()
		if err != nil {
			if err == io.EOF {
				return true, nil
			}
			return false, err
		}

		inst, err := r.CompileExpr(expr)
		if err != nil {
			return false, err
		}

		if x := v.Unify(inst.Value()); x.Err() != nil {
			return false, x.Err()
		}
	}
}
