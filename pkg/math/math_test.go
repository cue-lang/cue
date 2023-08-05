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

package math_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/pkg/internal/builtintest"
	"cuelang.org/go/pkg/math"
)

func TestBuiltin(t *testing.T) {
	builtintest.Run("math", t)
}

func Example_constants() {
	show := func(name string, value any) {
		fmt.Printf("% 7s: %v\n", name, value)
	}

	show("E", math.E)
	show("Pi", math.Pi)
	show("Phi", math.Phi)

	show("Sqrt2", math.Sqrt2)
	show("SqrtE", math.SqrtE)
	show("SqrtPi", math.SqrtPi)
	show("SqrtPhi", math.SqrtPhi)

	show("Ln2", math.Ln2)
	show("Log2E", math.Log2E)
	show("Ln10", math.Ln10)
	show("Log10E", math.Log10E)

	// Output:
	//       E: 2.718281828459045
	//      Pi: 3.141592653589793
	//     Phi: 1.618033988749895
	//   Sqrt2: 1.4142135623730951
	//   SqrtE: 1.6487212707001282
	//  SqrtPi: 1.772453850905516
	// SqrtPhi: 1.272019649514069
	//     Ln2: 0.6931471805599453
	//   Log2E: 1.4426950408889634
	//    Ln10: 2.302585092994046
	//  Log10E: 0.4342944819032518
}
