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
// Wasm is an experimental feature and the interface described in this
// document may change in the future.
//
// # Using Wasm modules in CUE
//
// To utilize Wasm modules, CUE files need to declare their intent by
// specifying a package attribute:
//
//	@extern("wasm")
//	package p
//
// Individual functions can then be imported from Wasm modules using
// a field attribute:
//
//	add: _ @extern("foo.wasm", abi=c, sig="func(int64, int64): int64")
//	mul: _ @extern("foo.wasm", abi=c, sig="func(float64, float64): float64")
//	not: _ @extern("foo.wasm", abi=c, sig="func(bool): bool")
//
// The first attribute argument specifies the file name of the Wasm
// module, which must reside in the same directory as the CUE file
// which uses it. The abi indicates the abstract binary interface (ABI)
// used by the function (see below) while sig indicates the type
// signature of the function. The grammar for sig is:
//
//	list    := expr [ { "," expr } ]
//	func    := "func" "(" [ list ] ")" ":" expr
//
// Where expr are all valid CUE identifiers and selectors.
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
// CUE runs Wasm code in a secure sandbox, which restricts access to
// external resources. Therefore, any Wasm code intended for execution
// in CUE must be self-contained and cannot have external dependencies.
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
// Wasm code must always terminate and return a result.
//
// Failure to provide the above guarantees will break the internal
// logic of CUE and will cause the CUE evaluation to be undefined.
//
// The CUE runtime may try to detect violations of the above rules,
// but it cannot provide any guarantees that violations will be detected.
// It is the responsability of the programmer to comply to the above
// requirements.
//
// # ABI requirements for Wasm modules
//
// Currently only the [System V ABI] (also known as the C ABI) is
// supported. Furthermore, only scalar data types and structs containing
// either scalar types or other structs can be exchanged between CUE
// and Wasm. Scalar means booleans, sized integers, and sized floats.
// The sig field in the attribute refers to these data types by their
// CUE names, such as bool, uint16, float64.
//
// Additionally the Wasm module must export two functions with the
// following C type signature:
//
//	void*	allocate(int n);
//	void	deallocate(void *ptr, int n);
//
// Allocate returns a Wasm pointer to a buffer of size n. Deallocate
// takes a Wasm pointer and the size of the buffer it points to and
// frees it.
//
// # How to compile Rust for use in CUE
//
// To compile Rust code into a Wasm module usable by CUE, make sure
// you have either the wasm32-unknown-unknown or wasm32-wasi targets
// installed:
//
//	rustup target add wasm32-wasi
//
// Note that even with wasm32-wasi, you should assume a [no_std]
// environment. Even though CUE can load [WASI] modules, the loaded
// modules do not currently have access to a WASI environment. This
// might change in the future.
//
// Compile your Rust crate using a cdynlib crate type as your [cargo target]
// targeting the installed Wasm target and make sure the functions you
// are exporting are using the C ABI, like so:
//
//	#[no_mangle]
//	pub extern "C" fn mul(a: f64, b: f64) -> f64 {
//	    a * b
//	}
//
// The following Rust functions can be used to implement allocate and
// deallocate described above:
//
//	#[cfg_attr(all(target_arch = "wasm32"), export_name = "allocate")]
//	#[no_mangle]
//	pub extern "C" fn _allocate(size: u32) -> *mut u8 {
//	    allocate(size as usize)
//	}
//
//	fn allocate(size: usize) -> *mut u8 {
//	    let vec: Vec<MaybeUninit<u8>> = Vec::with_capacity(size);
//
//	    Box::into_raw(vec.into_boxed_slice()) as *mut u8
//	}
//
//	#[cfg_attr(all(target_arch = "wasm32"), export_name = "deallocate")]
//	#[no_mangle]
//	pub unsafe extern "C" fn _deallocate(ptr: u32, size: u32) {
//	    deallocate(ptr as *mut u8, size as usize);
//	}
//
//	unsafe fn deallocate(ptr: *mut u8, size: usize) {
//	    let _ = Vec::from_raw_parts(ptr, 0, size);
//	}
//
// [System V ABI]: https://github.com/WebAssembly/tool-conventions/blob/main/BasicCABI.md
// [no_std]: https://docs.rust-embedded.org/book/intro/no-std.html
// [WASI]: https://wasi.dev
// [cargo target]: https://doc.rust-lang.org/cargo/reference/cargo-targets.html
package wasm
