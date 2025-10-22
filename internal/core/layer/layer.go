// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package layer

type Priority int8

// TODO: algorithm for handling disjunctions more intuitively with layering.
// For instance, how do we handle this case?:
//
// 		x: {} | *{
// 		  b: x: 1
// 		  c: x: 2
// 		}
// 		// 		If y > x, does the 2 of b force the default of x to fail? Could be an option.
// 		y: {
// 		  b: *{x: 2} | {}
// 		  c: *{x: 3} | {}
// 		}
