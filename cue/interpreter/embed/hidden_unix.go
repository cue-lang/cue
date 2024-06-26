// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build unix

package embed

import "path"

// isHidden checks if a file is hidden on Windows. We do not return an error
// if the file does not exist and will check that elsewhere.
func (c *compiler) isHidden(file string) bool {
	base := path.Base(file) // guaranteed to be non-empty
	return base[0] == '.'
}
