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

// Package tool defines statefull operation types for cue commands.
//
// This package is only visible in cue files with a _tool.cue or _tool_test.cue
// ending.
//
// CUE configuration files are not influenced by and do not influence anything
// outside the configuration itself: they are hermetic. Tools solve
// two problems: allow outside values such as environment variables,
// file or web contents, random generators etc. to influence configuration,
// and allow configuration to be actionable from within the tooling itself.
// Separating these concerns makes it clear to user when outside influences are
// in play and the tool definition can be strict about what is allowed.
//
// Tools are defined in files ending with _tool.cue. These files have a
// top-level map, "command", which defines all the tools made available through
// the cue command.
//
//
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

	// A var defines a value that can be set by the command line or an
	// environment variable.
	//
	// Example:
	//    var fast: {
	//        description: "run faster than anyone"
	//        value:       true | bool
	//    }
	//
	var <name>: {
		value:       _
		description: "" | string
	}

	// tasks specifies the list of things to do to run command. Tasks are
	// typically underspecified and completed by the particular internal
	// handler that is running them. Task de
	tasks <name>: Task

	// TODO:
	// timeout?: number // in seconds
}

// A task defines a step in the execution of a command, server, or fix
// operation.
Task: {
	kind: =~#"\."#
}

// import "cue/tool"
//
// command <Name>: { // from "cue/tool".Command
//  // usage gives a short usage pattern of the command.
//  // Example:
//  //    fmt [-n] [-x] [packages]
//  usage: Name | string
//
//  // short gives a brief on-line description of the command.
//  // Example:
//  //    reformat package sources
//  short: "" | string
//
//  // long gives a detailed description of the command, including a
//  // description of flags usage and examples.
//  long: "" | string
//
//  // A task defines a single action to be run as part of this command.
//  // Each task can have inputs and outputs, depending on the type
//  // task. The outputs are initially unspecified, but are filled out
//  // by the tooling
//  //
//  task <Name>: { // from "cue/tool".Task
//   // supported fields depend on type
//  }
//
//  VarValue = string | bool | int | float | [...string|int|float]
//
//  // var declares values that can be set by command line flags or
//  // environment variables.
//  //
//  // Example:
//  //   // environment to run in
//  //   var env: "test" | "prod"
//  // The tool would print documentation of this flag as:
//  //   Flags:
//  //      --env string    environment to run in: test(default) or prod
//  var <Name>: VarValue
//
//  // flag defines a command line flag.
//  //
//  // Example:
//  //   var env: "test" | "prod"
//  //
//  //   // augment the flag information for var
//  //   flag env: {
//  //       shortFlag:   "e"
//  //       description: "environment to run in"
//  //   }
//  //
//  // The tool would print documentation of this flag as:
//  //   Flags:
//  // -e, --env string    environment to run in: test(default), staging, or prod
//  //
//  flag <Name>: { // from "cue/tool".Flag
//   // value defines the possible values for this flag.
//   // The default is string. Users can define default values by
//   // using disjunctions.
//   value: env[Name].value | VarValue
//
//   // name, if set, allows var to be set with the command-line flag
//   // of the given name. null disables the command line flag.
//   name: Name | null | string
//
//   // short defines an abbreviated version of the flag.
//   // Disabled by default.
//   short: null | string
//  }
//
//  // populate flag with the default values for
//  flag: { "\(k)": { value: v } | null for k, v in var }
//
//  // env defines environment variables. It is populated with values
//  // for var.
//  //
//  // To specify a var without an equivalent environment variable,
//  // either specify it as a flag directly or disable the equally
//  // named env entry explicitly:
//  //
//  //     var foo: string
//  //     env foo: null  // don't use environment variables for foo
//  //
//  env <Name>: {å
//   // name defines the environment variable that sets this flag.
//   name: "CUE_VAR_" + strings.Upper(Name) | string | null
//
//   // The value retrieved from the environment variable or null
//   // if not set.
//   value: null | string | bytes
//  }
//  env: { "\(k)": { value: v } | null for k, v in var }
// }
//
