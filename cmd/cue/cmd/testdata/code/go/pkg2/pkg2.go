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

// Package pkg2 does other stuff.
package pkg2

import (
	"math/big"
	t "time"
)

// A Barzer barzes.
type Barzer struct {
	A int `protobuf:"varint,2," json:"a"`

	T t.Time
	B *big.Int
	C big.Int
	F big.Float `xml:",attr"`
	G *big.Float
	H bool `json:"-"`
	S string

	Err error
}

const Perm = 0755

const Few = 3

const Couple int = 2
