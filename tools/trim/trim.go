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

package trim

import (
	"io"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

// Config configures trim options.
type Config struct {
	Trace       bool
	TraceWriter io.Writer
}

func Files(files []*ast.File, inst cue.InstanceOrValue, cfg *Config) error {
	val := inst.Value()
	return filesV2(files, val, cfg)
}
