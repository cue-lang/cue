// Copyright 2025 CUE Authors
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

// Package astsentinel provides sentinel values for AST nodes.
package astsentinel

// Predeclared is a sentinel node used to mark identifiers that refer to
// predeclared names such as "self" or "int".
//
// When [cuelang.org/go/cue/ast/astutil.Sanitize] encounters an identifier
// whose Node is this sentinel and the name is shadowed in scope, it renames
// the identifier to avoid the shadow.
//
// This variable is set by the ast package during initialization.
var Predeclared any
