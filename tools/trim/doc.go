// Copyright 2025 The CUE Authors
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

// Package trim removes some redundant values from CUE sources.
//
// The most common use of trim is to remove duplicate values from data
// where a definition now provides the same value. For example:
//
//	servers: [
//		{role: "web", cpus: 1},
//		{role: "db", cpus: 1},
//		{role: "proxy", cpus: 1},
//	]
//
//	#server: {
//		role: string
//		cpus: 1
//	}
//
//	servers: [...#server]
//
// Trim will simplify this to:
//
//	servers: [
//		{role: "web"},
//		{role: "db"},
//		{role: "proxy"},
//	]
//
//	#server: {
//		role: string
//		cpus: 1
//	}
//
//	servers: [...#server]
//
// This works with defaults too. Given:
//
//	servers: [
//		{role: "web", cpus: 1},
//		{role: "db", cpus: 4},
//		{role: "proxy", cpus: 1},
//	]
//
//	#server: {
//		role: string
//		cpus: *1 | int
//	}
//
//	servers: [...#server]
//
// Trim will simplify this to:
//
//	servers: [
//		{role: "web"},
//		{role: "db", cpus: 4},
//		{role: "proxy"},
//	]
//
//	#server: {
//		role: string
//		cpus: *1 | int
//	}
//
//	servers: [...#server]
package trim
