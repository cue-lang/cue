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

// Package rules:
//
// - the package clause defines a namespace.
// - a cue file without a package clause is a standalone file.
// - all files with the same package name within a directory and its
//   ancestor directories up to the package root belong to the same package.
// - The package root is either the top of the file hierarchy or the first
//   directory in which a cue.mod file is defined.
//
// The contents of a namespace depends on the directory that is selected as the
// starting point to load a package. An instance defines a package-directory
// pair.
