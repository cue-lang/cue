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
	"cuelang.org/go/internal/core/adt"
	cueyaml "cuelang.org/go/internal/encoding/yaml"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/value"
)

// Marshal returns the YAML encoding of v.
func Marshal(v cue.Value) (string, error) {
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return "", err
	}
	n := v.Syntax(cue.Concrete(true))
	b, err := cueyaml.Encode(n)
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
		v := iter.Value()
		if err := v.Validate(cue.Concrete(true)); err != nil {
			return "", err
		}
		n := v.Syntax(cue.Concrete(true))
		b, err := cueyaml.Encode(n)
		if err != nil {
			return "", err
		}
		buf.Write(b)
	}
	return buf.String(), nil
}

// Unmarshal parses the YAML to a CUE expression.
func Unmarshal(data []byte) (ast.Expr, error) {
	return cueyaml.Unmarshal("", data)
}

// UnmarshalStream parses the YAML to a CUE list expression on success.
func UnmarshalStream(data []byte) (ast.Expr, error) {
	d := cueyaml.NewDecoder("", data)
	a := []ast.Expr{}
	for {
		x, err := d.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		a = append(a, x)
	}

	return ast.NewList(a...), nil
}

// Validate validates YAML and confirms it is an instance of schema.
// If the YAML source is a stream, every object must match v.
func Validate(b []byte, v pkg.Schema) (bool, error) {
	// This function is left for Go documentation. The package entry calls
	// cueyaml.Validate directly, passing it the call context.

	ctx := value.OpContext(v)
	return cueyaml.Validate(ctx, b, v)
}

// validate is the actual implementation of Validate.
func validate(c *adt.OpContext, b []byte, v pkg.Schema) (bool, error) {
	return cueyaml.Validate(c, b, v)
}

// ValidatePartial validates YAML and confirms it matches the constraints
// specified by v using unification. This means that b must be consistent with,
// but does not have to be an instance of v. If the YAML source is a stream,
// every object must match v.
func ValidatePartial(b []byte, v pkg.Schema) (bool, error) {
	// This function is left for Go documentation. The package entry calls
	// cueyaml.ValidatePartial directly, passing it the call context.

	ctx := value.OpContext(v)
	return cueyaml.ValidatePartial(ctx, b, v)
}

// validatePartial is the actual implementation of ValidatePartial.
func validatePartial(c *adt.OpContext, b []byte, v pkg.Schema) (bool, error) {
	return cueyaml.ValidatePartial(c, b, v)
}
