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

package cache

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/unstable/lsp/eval"
)

// TestCompletionItemKind: each kind of the evaluator's completions is
// sent as the protocol's kind of the same name, and a field is still
// ordered before a variable of the same label.
func TestCompletionItemKind(t *testing.T) {
	field := completionItemKind(eval.FieldCompletion)
	variable := completionItemKind(eval.VariableCompletion)
	qt.Assert(t, qt.Equals(field, protocol.FieldCompletion))
	qt.Assert(t, qt.Equals(variable, protocol.VariableCompletion))
	qt.Assert(t, qt.IsTrue(field < variable))
	qt.Assert(t, qt.Equals(completionItemKind(0), protocol.CompletionItemKind(0)))
}

func TestCompareLocations(t *testing.T) {
	const src = protocol.DocumentURI("file:///src.cue")
	loc := func(endChar uint32) protocol.Location {
		return protocol.Location{URI: src, Range: protocol.Range{
			Start: protocol.Position{Line: 1, Character: 2},
			End:   protocol.Position{Line: 1, Character: endChar},
		}}
	}
	// Locations with equal starts are ordered by their ends.
	short, long := loc(3), loc(5)
	qt.Assert(t, qt.Equals(compareLocations(src, short, long), -1))
	qt.Assert(t, qt.Equals(compareLocations(src, long, short), 1))
	qt.Assert(t, qt.Equals(compareLocations(src, short, short), 0))
}
