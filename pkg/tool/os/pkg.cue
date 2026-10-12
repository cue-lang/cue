// Copyright 2026 The CUE Authors
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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

// Package os defines tasks for retrieving os-related information.
//
// CUE definitions:
package os

// A Value is a value allowed for an environment variable.
// A null value indicates an unset environment variable.
Value: bool | number | *string | null

// Name indicates a valid environment variable name.
Name: !="" & !~"^[$]"

// Setenv is meant to set environment variables, but it is not implemented:
// running it in a command fails. To set environment variables for a
// command run by tool/exec.Run, use its env field.
Setenv: {
	$id: _id
	_id: "tool/os.Setenv"

	{[Name]: Value}
}

// Getenv gets and parses the specified environment variables.
Getenv: {
	$id: _id
	_id: "tool/os.Getenv"

	{[Name]: Value}
}

// Environ populates a struct with all environment variables.
Environ: {
	$id: _id
	_id: "tool/os.Environ"

	// A map of all populated values.
	// Individual entries may be specified ahead of time to enable
	// validation and parsing. Values that are marked as required
	// will fail the task if they are not found.
	{[Name]: Value}
}

// Clearenv is meant to clear all environment variables, but it is not
// implemented: running it in a command fails.
Clearenv: {
	$id: _id
	_id: "tool/os.Clearenv"
}
