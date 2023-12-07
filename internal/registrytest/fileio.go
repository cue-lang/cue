// Copyright 2023 CUE Authors
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

package registrytest

import (
	"bytes"
	"io"
	"os"
	"path"
	"time"

	"golang.org/x/tools/txtar"
)

// txtarFileIO implements mod/zip.FileIO[txtar.File].
type txtarFileIO struct{}

func (txtarFileIO) Path(f txtar.File) string {
	return f.Name
}

func (txtarFileIO) Lstat(f txtar.File) (os.FileInfo, error) {
	return txtarFileInfo{f}, nil
}

func (txtarFileIO) Open(f txtar.File) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.Data)), nil
}

func (txtarFileIO) Mode() os.FileMode {
	return 0o444
}

type txtarFileInfo struct {
	f txtar.File
}

func (fi txtarFileInfo) Name() string {
	return path.Base(fi.f.Name)
}

func (fi txtarFileInfo) Size() int64 {
	return int64(len(fi.f.Data))
}

func (fi txtarFileInfo) Mode() os.FileMode {
	return 0o644
}

func (fi txtarFileInfo) ModTime() time.Time { return time.Time{} }
func (fi txtarFileInfo) IsDir() bool        { return false }
func (fi txtarFileInfo) Sys() interface{}   { return nil }
