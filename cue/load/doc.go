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

// Package load loads CUE instances.
package load

// Trigger the unconditional loading of all core builtin packages if load is used.
// This was deemed the simplest way to avoid having to import this line explicitly,
// and thus breaking existing code, for the majority of cases,
// while not introducing an import cycle.
import _ "cuelang.org/go/pkg"
