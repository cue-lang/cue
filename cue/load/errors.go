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

package load

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// A PackageError describes an error loading information about a package.
type PackageError struct {
	ImportStack    []string  // shortest path from package named on command line to this one
	Pos            token.Pos // position of error
	errors.Message           // the error itself
	IsImportCycle  bool      // the error is an import cycle
}

func (p *PackageError) Position() token.Pos         { return p.Pos }
func (p *PackageError) InputPositions() []token.Pos { return nil }
func (p *PackageError) Path() []string              { return p.ImportStack }

func (p *PackageError) fillPos(cwd string, positions []token.Pos) {
	if len(positions) > 0 && !p.Pos.IsValid() {
		p.Pos = positions[0]
	}
}

// TODO(localize)
func (p *PackageError) Error() string {
	// Import cycles deserve special treatment.
	if p.IsImportCycle {
		return fmt.Sprintf("%s\npackage %s\n", p.Message, strings.Join(p.ImportStack, "\n\timports "))
	}
	if p.Pos.IsValid() {
		// Omit import stack. The full path to the file where the error
		// is the most important thing.
		return p.Pos.String() + ": " + p.Message.Error()
	}
	if len(p.ImportStack) == 0 {
		return p.Message.Error()
	}
	return "package " + strings.Join(p.ImportStack, "\n\timports ") + ": " + p.Message.Error()
}
