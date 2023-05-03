// Copyright 2023 CUE Authors
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

// Package wasm allows users to write their own functions and make
// them available to CUE via Wasm modules.
//
// To enable Wasm support, pass the result of [New] to
// [cuelang.org/go/cue/cuecontext.New]. Wasm is enabled by default in
// the command line tool.
//
// # Using Wasm modules in CUE
//
// CUE files that wish to use Wasm modules must declare their intent
// via a package attribute, like so:
//
//	@extern("wasm")
//	package p
//
// Individual functions can be then imported from Wasm modules using a field attribute, like so:
//
//	add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")
//	mul: _ @extern("foo.wasm", abi=c, sig="func(float64, float64): float64")
//	not: _ @extern("foo.wasm", abi=c, sig="func(bool): bool")
//
// The first attribute argument specifies the file name of Wasm
// module, which must reside in the same directory as the CUE file
// which uses it. The abi indicates the ABI used by the function
// (more about that in a later section). Sig indicates the type signature
// of the function. The grammar for sig is:
//
//	letter  := /* Unicode "Letter" */ .
//	digit   := /* Unicode "Number, decimal digit" */ .
//	alpha   := letter | digit .
//	ident   := alpha { alpha } .
//	list    := ident [ { "," ident } ]
//	func    := "func" "(" [ list ] ")" ":" ident
//
// The specific ABI used may restrict the allowable signatures further.
//
// By default, the named Wasm module is searched for a function with
// the same name as the CUE field that is associated with the attribute.
// If you want to import a function under a different name, you can
// specify this in the attribute using an optional name parameter, for
// example:
//
//	isPrime: _ @extern("bar.wasm", abi=c, name=is_prime, sig="func(uint64): bool")
//
// # Runtime requirements for Wasm modules
//
// CUE runs Wasm code in a sandbox, with no access to the outside
// world, so any Wasm code must be self-contained (no dependencies).
//
// All code exported for use by CUE must be free of observable side
// effects. The result of a function call must depend only on its
// arguments, and no other implicit state. If a function uses global
// state, it must do so only in a way that is undetectable from the
// outside. For example, a function that caches results to speed up
// its future invocations (memoization) is permitted, but a function
// that returns a random number is not.
//
// The CUE runtime may run different function invocations in different
// Wasm runtime instances, so Wasm code must not depend on the existence
// of shared state between different function invocations.
//
// Wasm code must always return a result, a function invocation must
// not loop indefinitely.
//
// Failure to provide the above guarantees will break the internal
// logic of CUE.
//
// The CUE runtime may try to detect violations of the above rules,
// but it cannot provide any guarantees that violations will be detected.
// It is the respnsability of the programmer to comply to the above
// requirements.
//
// # ABI requirements for Wasm modules
//
// Currently only the [C ABI] is supported. Furthermore, only scalar
// data types can be exchanged between CUE and Wasm. That means booleans,
// sized integers and sized floats. The sig field in the attribute
// refers to these data types by their CUE names, such as bool, uint16,
// float64, etc.
//
// # How to compile Rust for use in CUE
//
// To compile Rust code into a Wasm module usable by CUE, make sure
// you have either the wasm32-unknown-unknown or wasm32-wasi targets
// instal;ed:
//
//	rustup target add wasm32-wasi
//
// Note that even with wasm32-wasi, you should assume a [no_std]
// environment. Even though CUE can load [WASI] modules, the loaded
// modules do not currently have access to a WASI environment. This
// might change in the future.
//
// Compile your Rust crate using a cdynlib crate type as your [cargo
// target] targetting the installed Wasm target and make sure the
// functions you are exporting are using the C ABI, like so:
//
//	#[no_mangle]
//	pub extern "C" fn mul(a: f64, b: f64) -> f64 {
//	    a * b
//	}
//
// [C ABI]: https://github.com/WebAssembly/tool-conventions/blob/main/BasicCABI.md
// [no_std]: https://docs.rust-embedded.org/book/intro/no-std.html
// [WASI]: https://wasi.dev
// [cargo target]: https://doc.rust-lang.org/cargo/reference/cargo-targets.html
package wasm
