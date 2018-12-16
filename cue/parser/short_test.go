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

// This file contains test cases for short valid and invalid programs.

package parser

import "testing"

var valids = []string{
	"\n",
	`{}`,
	`{ <Name>: foo }`,
	`{ a: 3 }`,
}

func TestValid(t *testing.T) {
	for _, src := range valids {
		t.Run(src, func(t *testing.T) {
			checkErrors(t, src, src)
		})
	}
}

func TestInvalid(t *testing.T) {
	invalids := []string{
		`foo !/* ERROR "expected label or ':', found '!'" */`,
		// `foo: /* ERROR "expected operand, found '}'" */}`, // TODO: wrong position
		`{ <Name
			/* ERROR "expected '>', found newline" */ >: foo }`,
		// TODO:
		// `{ </* ERROR "expected identifier, found newline" */
		// 	Name>: foo }`,
	}
	for _, src := range invalids {
		checkErrors(t, src, src)
	}
}
