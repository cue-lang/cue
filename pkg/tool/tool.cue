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

package tool

// A Command specifies a user-defined command.
Command: {
	//
	// Example:
	//     mycmd [-n] names
	usage?: string

	// short is short description of what the command does.
	short?: string

	// long is a longer description that spans multiple lines and
	// likely contain examples of usage of the command.
	long?: string

	// TODO: define flags and environment variables.

	// tasks specifies the list of things to do to run command. Tasks are
	// typically underspecified and completed by the particular internal
	// handler that is running them. Task de
	tasks <name>: Task
}

// A Task defines a step in the execution of a command.
Task: {
	// kind indicates the operation to run. It must be of the form
	// packagePath.Operation.
	kind: =~#"\."#
}
