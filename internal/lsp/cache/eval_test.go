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
)

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
