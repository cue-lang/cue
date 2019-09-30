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

package cmd

import (
	"flag"
	"os"
	"testing"

	"cuelang.org/go/cue/errors"
)

var _ = errors.Print

var update = flag.Bool("update", false, "update the test files")

func printConfig(t *testing.T) *errors.Config {
	t.Helper()

	inTest = true

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return &errors.Config{
		Cwd:     cwd,
		ToSlash: true,
	}
}
