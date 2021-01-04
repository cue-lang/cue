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

package adt_test

import (
	"strconv"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

func TestFeatureBool(t *testing.T) {
	r := runtime.New()
	ctx := adt.NewContext(r, &adt.Vertex{})

	makeInt := func(x int64) adt.Feature {
		f, _ := adt.MakeLabel(nil, 2, adt.IntLabel)
		return f
	}

	testCases := []struct {
		in           adt.Feature
		isRegular    bool
		isDefinition bool
		isHidden     bool
		isString     bool
		isInt        bool
	}{{
		in:        ctx.StringLabel("foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("_"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("_#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "_"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "_#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        makeInt(4),
		isRegular: true,
		isInt:     true,
	}, {
		in:        adt.MakeIdentLabel(r, "foo", "main"),
		isRegular: true,
		isString:  true,
	}, {
		in:           adt.MakeIdentLabel(r, "#foo", "main"),
		isDefinition: true,
	}, {
		in:           adt.MakeIdentLabel(r, "_#foo", "main"),
		isDefinition: true,
		isHidden:     true,
	}, {
		in:       adt.MakeIdentLabel(r, "_foo", "main"),
		isHidden: true,
	}}
	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := tc.in.IsRegular(); got != tc.isRegular {
				t.Errorf("IsRegular: got %v; want %v", got, tc.isRegular)
			}
			if got := tc.in.IsString(); got != tc.isString {
				t.Errorf("IsString: got %v; want %v", got, tc.isString)
			}
			if got := tc.in.IsInt(); got != tc.isInt {
				t.Errorf("IsInt: got %v; want %v", got, tc.isInt)
			}
			if got := tc.in.IsDef(); got != tc.isDefinition {
				t.Errorf("isDefinition: got %v; want %v", got, tc.isDefinition)
			}
			if got := tc.in.IsHidden(); got != tc.isHidden {
				t.Errorf("IsHidden: got %v; want %v", got, tc.isHidden)
			}
			if got := tc.in.IsString(); got != tc.isString {
				t.Errorf("IsString: got %v; want %v", got, tc.isString)
			}
		})
	}
}
