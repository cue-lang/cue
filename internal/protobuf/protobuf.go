// Copyright 2019 CUE Authors
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

// Package protobuf defines functionality for parsing protocol buffer
// definitions and instances.
//
// TODO: this package can become public once we have found a good nest for it.
package protobuf

import (
	"cuelang.org/go/cue/ast"
)

// Config specifies the environment into which to parse a proto definition file.
type Config struct {
	Paths []string
}

// Parse parses a single proto file and returns its contents translated to a CUE
// file. Imports are resolved using the paths defined in Config. If src is not
// nil, it will use this as the contents of the file. It may be a string, []byte
// or io.Reader. Otherwise Parse will open the given file name at the fully
// qualified path.
//
// The following field options are supported:
//    (cue.val)     string        CUE constraint for this field. The string may
//                                refer to other fields in a message definition.
//    (cue.opt)     FieldOptions
//       required   bool          Defines the field is required. Use with
//                                caution.
func Parse(filename string, src interface{}, c *Config) (f *ast.File, err error) {
	state := &sharedState{
		paths: c.Paths,
	}
	p, err := state.parse(filename, src)
	if err != nil {
		return nil, err
	}
	return p.file, nil
}

// TODO
// func GenDefinition

// func MarshalText(cue.Value) (string, error) {
// 	return "", nil
// }

// func MarshalBytes(cue.Value) ([]byte, error) {
// 	return nil, nil
// }

// func UnmarshalText(descriptor cue.Value, b string) (ast.Expr, error) {
// 	return nil, nil
// }

// func UnmarshalBytes(descriptor cue.Value, b []byte) (ast.Expr, error) {
// 	return nil, nil
// }
