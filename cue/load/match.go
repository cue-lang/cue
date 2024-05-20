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
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/cueimports"
)

// A match represents the result of matching a single package pattern.
type match struct {
	Pattern string // the pattern itself
	Literal bool   // whether it is a literal (no wildcards)
	Pkgs    []*build.Instance
	Err     errors.Error
}

var errExclude = errors.New("file rejected")

type cueError = errors.Error
type excludeError struct {
	cueError
}

func (e excludeError) Is(err error) bool { return err == errExclude }

// matchFile determines whether the file with the given name in the given directory
// should be included in the package being constructed.
// It returns the data read from the file.
// If returnImports is true and name denotes a CUE file, matchFile reads
// until the end of the imports (and returns that data) even though it only
// considers text until the first non-comment.
// If allTags is non-nil, matchFile records any encountered build tag
// by setting allTags[tag] = true.
func matchFile(cfg *Config, file *build.File, returnImports bool, allTags map[string]bool, mode importMode) (match bool, data []byte, err errors.Error) {
	// Note: file.Source should already have been set by setFileSource just
	// after the build.File value was created.
	if file.Encoding != build.CUE {
		return false, nil, nil // not a CUE file, don't record.
	}
	if file.Filename == "-" {
		return true, file.Source.([]byte), nil // don't check shouldBuild for stdin
	}

	name := filepath.Base(file.Filename)
	if (mode & allowExcludedFiles) == 0 {
		for _, prefix := range []string{".", "_"} {
			if strings.HasPrefix(name, prefix) {
				return false, nil, &excludeError{
					errors.Newf(token.NoPos, "filename starts with a '%s'", prefix),
				}
			}
		}
	}

	f, err := cfg.fileSystem.openFile(file.Filename)
	if err != nil {
		return false, nil, err
	}

	data, err = cueimports.Read(f)
	f.Close()
	if err != nil {
		return false, nil,
			errors.Newf(token.NoPos, "read %s: %v", file.Filename, err)
	}

	return true, data, nil
}
