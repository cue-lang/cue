// Copyright 2023 The CUE Authors
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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/load/internal/fileprocessor"
)

type (
	Source               = fileprocessor.Source
	TagVar               = fileprocessor.TagVar
	NoFilesError         = fileprocessor.NoFilesError
	MultiplePackageError = fileprocessor.MultiplePackageError
)

func FromFile(f *ast.File) Source {
	return fileprocessor.FromFile(f)
}

func FromString(s string) Source {
	return fileprocessor.FromString(s)
}

func FromBytes(s []byte) Source {
	return fileprocessor.FromBytes(s)
}
