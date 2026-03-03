// Copyright 2026 CUE Authors
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

// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package validatorconfig

import (
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

type File struct {
	ActiveProfile string              `json:"activeProfile"`
	Profiles      map[string]*Profile `json:"profiles,omitempty"`
}

type Profile struct {
	ServerURL string `json:"serverURL"`
	Name      string `json:"name"`
	Token     string `json:"token"`
}

// Parse reads the provided path as a data-only CUE file, and attempts
// to decode it into [File].
func Parse(path string) (file *File, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ctx := cuecontext.New()
	astFile, err := parseDataOnlyCUE(ctx, data, path)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid config file syntax")
	}
	// TODO: unify with a closed schema so that we can detect spelling
	// mistakes.
	v := ctx.BuildFile(astFile)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module file value")
	}
	var mf File
	if err := v.Decode(&mf); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct")
	}
	return &mf, nil
}

func parseDataOnlyCUE(ctx *cue.Context, cueData []byte, filename string) (*ast.File, error) {
	dec := encoding.NewDecoder(ctx, &build.File{
		Filename:       filename,
		Encoding:       build.CUE,
		Interpretation: build.Auto,
		Form:           build.Data,
		Source:         cueData,
	}, &encoding.Config{
		Mode:      filetypes.Export,
		AllErrors: true,
	})
	if err := dec.Err(); err != nil {
		return nil, err
	}
	return dec.File(), nil
}
