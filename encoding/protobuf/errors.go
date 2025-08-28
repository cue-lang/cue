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

package protobuf

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// protobufError implements cue/Error
type protobufError struct {
	path []string
	pos  token.Pos
	err  error
}

func (e *protobufError) Position() token.Pos {
	return e.pos
}

func (e *protobufError) InputPositions() []token.Pos {
	return nil
}

func (e *protobufError) Error() string {
	return errors.String(e)
}

func (e *protobufError) Path() []string {
	return e.path
}

func (e *protobufError) WriteError(w *strings.Builder, cfg *errors.Config) {
	fmt.Fprintf(w, "error parsing protobuf: %v", e.err)
}
