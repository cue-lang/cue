// Copyright 2020 CUE Authors
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

// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ospath_test

import (
	"fmt"

	"cuelang.org/go/internal/ospath"
)

func ExampleExt() {
	fmt.Printf("No dots: %q\n", ospath.Unix.Ext("index"))
	fmt.Printf("One dot: %q\n", ospath.Unix.Ext("index.js"))
	fmt.Printf("Two dots: %q\n", ospath.Unix.Ext("main.test.js"))
	// Output:
	// No dots: ""
	// One dot: ".js"
	// Two dots: ".js"
}
