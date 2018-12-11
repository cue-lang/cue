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

package path

import "path"

var split = path.Split

// Split splits path immediately following the final slash and returns them as
// the list [dir, file], separating it into a directory and file name component.
// If there is no slash in path, Split returns an empty dir and file set to
// path. The returned values have the property that path = dir+file.
func Split(path string) []string {
	file, dir := split(path)
	return []string{file, dir}
}
