// Copyright 2019 CUE Authors
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

// Package cuedata converts CUE to a literal data format where non-concrete
// CUE expressions in a struct are encoded as a string into a $$cue field.
//
// CUEdata literal format allows for CUE files to be stored and transmitted
// as structured data, such as in document databases, hence concrete values
// queried, and results can be decoded into CUE
package cuedata
