// Copyright 2025 The CUE Authors
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
	"io/fs"
	"path"
	"sync"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

type modFileCache struct {
	mu       sync.Mutex
	modfiles map[module.Version]*modfile.File
}

func newModFileCache() *modFileCache {
	return &modFileCache{
		modfiles: make(map[module.Version]*modfile.File),
	}
}

func (mc *modFileCache) modFile(mv module.Version, loc module.SourceLoc) (*modfile.File, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if loc.FS == nil {
		return nil, fmt.Errorf("no location for %v", mv)
	}
	if f := mc.modfiles[mv]; f != nil {
		return f, nil
	}
	data, err := fs.ReadFile(loc.FS, path.Join(loc.Dir, "cue.mod/module.cue"))
	if err != nil {
		return nil, err
	}
	f, err := modfile.Parse(data, "cue.mod/module.cue")
	if err != nil {
		return nil, fmt.Errorf("%v: %v", mv, err)
	}
	mc.modfiles[mv] = f
	return f, nil
}
