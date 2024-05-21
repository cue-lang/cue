// Copyright 2024 The CUE Authors
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

package cuelang

import (
	"bytes"
	"context"

	cueformat "cuelang.org/go/cue/format"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/diff"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

// FormatCUE formats a CUE file with a given range.
func FormatCUE(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle) ([]protocol.TextEdit, error) {
	ctx, done := event.Start(ctx, "source.FormatCUE")
	defer done()

	// TODO cache the parsed artefacts, which will include the mapper

	src, err := fh.Content()
	if err != nil {
		return nil, err
	}
	res, err := cueformat.Source(src)
	if err != nil {
		// TODO fix up the AST like gopls so we can do more with
		// partial/incomplete code.
		//
		// For now return early because there is nothing we can do.
		return nil, nil
	}

	// If the format did nothing, do nothing
	if bytes.Equal(src, res) {
		return nil, nil
	}

	mapper := protocol.NewMapper(fh.URI(), src)
	edits := diff.Strings(string(src), string(res))
	return protocol.EditsFromDiffEdits(mapper, edits)
}
