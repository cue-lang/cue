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

package pkg1

import (
	"encoding"
	"encoding/json"
	"time"

	p2 "cuelang.org/go/cmd/cue/cmd/testdata/code/go/pkg2"
)

// Foozer foozes a jaman.
type Foozer struct {
	Int    int
	String string

	Inline `json:",inline"`
	NoInline

	CustomJSON CustomJSON
	CustomYAML *CustomYAML
	AnyJSON    json.Marshaler
	AnyText    encoding.TextMarshaler

	Bar int `json:"bar,omitempty"`

	exclude int

	// Time is mapped to CUE's internal type.
	Time time.Time

	Barzer p2.Barzer

	Map    map[string]*CustomJSON
	Slice1 []int
	Slice2 []interface{}
	Slice3 *[]json.Unmarshaler
	Array1 [5]int
	Array2 [5]interface{}
	Array3 *[5]json.Marshaler

	Intf  Interface `protobuf:"varint,2,name=intf"`
	Intf2 interface{}
	Intf3 struct{ Interface }
	Intf4 interface{ Foo() }

	// Even though this struct as a type implements MarshalJSON, it is known
	// that it is really only implemented by the embedded field.
	Embed struct{ CustomJSON }

	Unsupported map[int]string
}

// Level gives an indication of the extent of stuff.
type Level int

const (
	/*
		Block comment.
			Indented.

		Empty line before.
	*/
	Unknown Level = iota
	Low
	// Medium is neither High nor Low
	Medium
	High
)

type CustomJSON struct {
}

func (c *CustomJSON) MarshalJSON() ([]byte, error) {
	return nil, nil
}

type CustomYAML struct {
}

func (c CustomYAML) MarshalYAML() ([]byte, error) {
	return nil, nil
}

type excludeType int

type Inline struct {
	Kind string
}

type NoInline struct {
	Kind string
}

type Interface interface {
	Boomer() bool
}
