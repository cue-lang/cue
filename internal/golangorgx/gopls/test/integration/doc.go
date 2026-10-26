// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package integration provides a framework for writing integration tests of
// the CUE LSP server.
//
// The behaviors that matter to users, and the scenarios they
// typically describe in bug report, are usually expressed in terms of
// editor interactions. For example: "When I open my editor in this
// directory, navigate to this file, and change this line, I get a
// diagnostic that doesn't make sense". The integration package
// provides an API for maintainers to express these types of
// user interactions in ordinary Go tests, validate them, and run them
// in a variety of execution modes.
//
// # Test package setup
//
// The integration test package uses a couple of uncommon patterns to reduce
// boilerplate in test bodies. First, it is intended to be imported as "." so
// that helpers do not need to be qualified. Second, it requires some setup
// that is currently implemented in the integration.Main function, which must be
// invoked by TestMain. Therefore, a minimal integration testing package looks
// like this:
//
//	package feature
//
//	import (
//		"fmt"
//		"testing"
//
//		. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
//	)
//
//	func TestMain(m *testing.M) {
//		Main(m)
//	}
//
// # Writing a simple integration test
//
// To run an integration test use the integration.Run function, which accepts a
// txtar-encoded archive defining the initial workspace state. This function
// sets up the workspace in a temporary directory, creates a fake text editor,
// starts the server, and initializes an LSP session. It then invokes the provided
// test function with an *Env encapsulating the newly created
// environment. Because the server may be run in various modes (as a sidecar or
// daemon process, with different settings), the test runner may perform this
// process multiple times, re-running the test function each time with a new
// environment.
//
//	func TestOpenFile(t *testing.T) {
//		const files = `
//	-- cue.mod/module.cue --
//	module: "mod.example"
//	language: version: "v0.11.0"
//	-- foo.cue --
//	package foo
//	`
//		Run(t, files, func(t *testing.T, env *Env) {
//			env.OpenFile("foo.cue")
//		})
//	}
//
// # Configuring integration test execution
//
// The integration package exposes several options that affect the setup process
// described above. To use these options, use the WithOptions function:
//
//	WithOptions(opts...).Run(...)
//
// See options.go for a full list of available options.
//
// # Operating on editor state
//
// To operate on editor state within the test body, the Env type provides
// access to the workspace directory (Env.SandBox), text editor (Env.Editor),
// LSP server (Env.Server), and 'awaiter' (Env.Awaiter).
//
// In most cases, operations on these primitive building blocks of the
// integration test environment expect a Context (which should be a child of
// env.Ctx), and return an error. To avoid boilerplate, the Env exposes a set
// of wrappers in wrappers.go for use in scripting:
//
//	env.CreateBuffer("c/c.cue", "")
//	env.EditBuffer("c/c.cue", protocol.TextEdit{
//		NewText: `package c`,
//	})
//
// These wrappers thread through Env.Ctx, and call t.Fatal on any errors.
//
// # Expressing expectations
//
// The general pattern for an integration test is to script interactions with the
// fake editor and sandbox, and assert that the server behaves correctly after each
// state change. Unfortunately, this is complicated by the fact that state
// changes are communicated to the server via unidirectional client->server
// notifications (didOpen, didChange, etc.), and resulting server behavior such
// as diagnostics, logs, or messages is communicated back via server->client
// notifications. Therefore, within integration tests we must be able to say "do
// this, and then eventually the server should do that". To achieve this, the
// integration package provides a framework for expressing conditions that must
// eventually be met, in terms of the Expectation type.
//
// To express the assertion that "eventually the server must meet these
// expectations", use env.Await(...):
//
//	env.RegexpReplace("x/x.cue", `package x`, `package y`)
//	env.Await(
//		env.DoneWithChange(),
//		Diagnostics(env.AtRegexp("x/x.cue", `package y`)),
//	)
//
// Await evaluates the provided expectations atomically, whenever the client
// receives a state-changing notification from the server, and returns once
// they are all met at the same time. See expectation.go for a full list of
// available expectations.
//
// The server uses verbose progress notifications to inform the client about
// ongoing work (see CompletedWork). Expectations such as DoneWithChange are
// met once the server is done processing the notifications sent so far, so
// awaiting them alongside other expectations ensures that those are checked
// against up-to-date server state.
//
// A problem with this model is that if the server never meets the provided
// expectations, the test runner will hang until the test timeout
// (which defaults to 10m). Await only fails early when an expectation becomes
// unmeetable, such as NoLogMatching once a matching log message arrives.
//
// # Tips & Tricks
//
// Here are some tips and tricks for working with integration tests:
//
//  1. Set the environment variable CUELSP_INTEGRATION_TEST_TIMEOUT=5s during
//     development, so that integration tests time out after 5 seconds.
//  2. Run tests with  -short. This will only run integration tests in the
//     default execution mode.
//  3. Use capture groups to narrow regexp positions. All regular-expression
//     based positions (such as AtRegexp) will match the position of
//     the first capture group, if any are provided. This can be used to
//     identify a specific position in the code for a pattern that may occur in
//     multiple places. For example `(foo): int` matches the position
//     of "foo" within the field declaration.
package integration
