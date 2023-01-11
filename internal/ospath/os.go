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

package ospath

// CurrentOS holds the OS of the local system.
var CurrentOS = currentOS

// These types have been designed to minimize the diffs with the original Go
// code, thereby minimizing potential toil in keeping them up to date.

type OS struct {
	osInfo
	Separator     byte
	ListSeparator byte
}

func (o OS) isWindows() bool {
	return o.Separator == '\\'
}

type osInfo interface {
	isPathSeparator(b byte) bool
	splitList(path string) []string
	volumeNameLen(path string) int
	isAbs(path string) (b bool)
	join(elem []string) string
	sameWord(a, b string) bool
}

var (
	Plan9 = OS{
		osInfo:        &plan9Info{},
		Separator:     plan9Separator,
		ListSeparator: plan9ListSeparator,
	}
	Unix = OS{
		osInfo:        &unixInfo{},
		Separator:     unixSeparator,
		ListSeparator: unixListSeparator,
	}
	Windows = OS{
		osInfo:        &windowsInfo{},
		Separator:     windowsSeparator,
		ListSeparator: windowsListSeparator,
	}
)
