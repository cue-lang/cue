// Copyright 2020 CUE Authors
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

package fix

import (
	"os"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
)

// Instances modifies all files contained in the given build instances at once.
//
// It also applies fix.File.
func Instances(a []*build.Instance, o ...Option) errors.Error {
	cwd, _ := os.Getwd()

	// Collect all
	p := processor{
		instances: a,
		cwd:       cwd,
	}

	p.visitAll(func(f *ast.File) { File(f, o...) })

	return p.err
}

type processor struct {
	instances []*build.Instance
	cwd       string

	err errors.Error
}

func (p *processor) visitAll(fn func(f *ast.File)) {
	if p.err != nil {
		return
	}

	done := map[*ast.File]bool{}

	for _, b := range p.instances {
		for _, f := range b.Files {
			if done[f] {
				continue
			}
			done[f] = true
			fn(f)
		}
	}
}
