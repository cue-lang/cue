// Copyright 2026 The CUE Authors
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

// Package langversion reports a @version attribute in a file of a module
// that declares a language version, where the module file decides the
// version of its files.
package langversion

import (
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// An Error reports a @version attribute in a file of a module that declares
// a language version. Commands which tolerate other load errors must not
// tolerate it.
type Error struct {
	cueError
}

// cueError allows embedding an [errors.Error] without its field shadowing
// the Error method.
type cueError = errors.Error

// AttrInModuleError returns the error for a @version attribute at pos in a
// file of the module whose module file is modFile, which declares language
// version declared.
func AttrInModuleError(pos token.Pos, modFile, declared string) errors.Error {
	return &Error{errors.Newf(pos, "the @version attribute cannot be used in a file of the module whose %s declares language version %s; the module file decides the version of its files", modFile, declared)}
}
