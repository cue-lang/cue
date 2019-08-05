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
	"time"

	"cuelang.org/go/encoding/gocode/testdata/pkg2"
)

type MyStruct struct {
	A int
	B string
	T time.Time // maps to builtin
	O *OtherStruct
	I *pkg2.ImportMe
}

type OtherStruct struct {
	A string
	// D time.Duration // maps to builtin
	P pkg2.PickMe
}

type String string

type Omit int

type Ptr *struct {
	A int
}
