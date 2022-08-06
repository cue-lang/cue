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

package parser

import (
	"io/ioutil"
	"testing"

	"cuelang.org/go/internal/source"
)

var src = readFile("testdata/commas.src")

func readFile(filename string) []byte {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	return data
}

func BenchmarkParse(b *testing.B) {
	b.SetBytes(int64(len(src)))
	for i := 0; i < b.N; i++ {
		if _, err := ParseFile("", source.NewBytesSource(src), ParseComments); err != nil {
			b.Fatalf("benchmark failed due to parse error: %s", err)
		}
	}
}
