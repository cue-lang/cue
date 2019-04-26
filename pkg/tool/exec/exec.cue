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

package exec

// Run executes the given shell command.
Run: {
	kind: *"tool/exec.Run" | "exec" // exec for backwards compatibility

	// cmd is the command to run.
	cmd: string | [string, ...string]

	// install is an optional command to install the binaries needed
	// to run the command.
	install?: string | [string, ...string]

	// env defines the environment variables to use for this system.
	env <Key>: string

	// stdout captures the output from stdout if it is of type bytes or string.
	// The default value of null indicates it is redirected to the stdout of the
	// current process.
	stdout: *null | string | bytes

	// stderr is like stdout, but for errors.
	stderr: *null | string | bytes

	// stdin specifies the input for the process.
	stdin?: string | bytes

	// success is set to true when the process terminates with with a zero exit
	// code or false otherwise. The user can explicitly specify the value
	// force a fatal error if the desired success code is not reached.
	success: bool
}
