// Copyright 2025 CUE Authors
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

package jsonschema

import (
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
)

func TestMergeAllOf(t *testing.T) {
	itemString := &itemType{kinds: []string{"string"}}
	itemNumber := &itemType{kinds: []string{"number"}}
	itemBool := &itemType{kinds: []string{"boolean"}}

	tests := []struct {
		name string
		item item
		want item
	}{
		{
			name: "non-allOf item returns as-is",
			item: itemString,
			want: itemString,
		},
		{
			name: "allOf with single element returns that element",
			item: &itemAllOf{
				elems: []item{itemString},
			},
			want: itemString,
		},
		{
			name: "allOf with multiple elements stays as allOf",
			item: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
		},
		{
			name: "nested allOf is flattened",
			item: &itemAllOf{
				elems: []item{
					itemString,
					&itemAllOf{
						elems: []item{itemNumber, itemBool},
					},
				},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber, itemBool},
			},
		},
		{
			name: "multiple nested allOf are all flattened",
			item: &itemAllOf{
				elems: []item{
					&itemAllOf{
						elems: []item{itemString},
					},
					&itemAllOf{
						elems: []item{itemNumber},
					},
					&itemAllOf{
						elems: []item{itemBool},
					},
				},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber, itemBool},
			},
		},
		{
			name: "deeply nested allOf is fully flattened",
			item: &itemAllOf{
				elems: []item{
					itemString,
					&itemAllOf{
						elems: []item{
							itemNumber,
							&itemAllOf{
								elems: []item{itemBool},
							},
						},
					},
				},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber, itemBool},
			},
		},
		{
			name: "duplicate items are removed",
			item: &itemAllOf{
				elems: []item{itemString, itemString, itemNumber, itemString},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
		},
		{
			name: "duplicate items after flattening are removed",
			item: &itemAllOf{
				elems: []item{
					itemString,
					&itemAllOf{
						elems: []item{itemString, itemNumber},
					},
					itemString,
				},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
		},
		{
			name: "allOf nested in other item types has children merged",
			item: &itemNot{
				elem: &itemAllOf{
					elems: []item{
						&itemAllOf{
							elems: []item{
								&itemAllOf{
									elems: []item{itemString},
								},
								itemNumber,
							},
						},
					},
				},
			},
			want: &itemNot{
				elem: &itemAllOf{
					elems: []item{itemString, itemNumber},
				},
			},
		},
		{
			name: "allOf nested in anyOf is recursively merged",
			item: &itemAnyOf{
				elems: []item{
					&itemAllOf{
						elems: []item{
							&itemAllOf{
								elems: []item{itemString},
							},
							itemNumber,
						},
					},
					itemBool,
				},
			},
			want: &itemAnyOf{
				elems: []item{
					&itemAllOf{
						elems: []item{itemString, itemNumber},
					},
					itemBool,
				},
			},
		},
		{
			name: "single element after flattening and deduplication",
			item: &itemAllOf{
				elems: []item{
					&itemAllOf{
						elems: []item{itemString},
					},
					&itemAllOf{
						elems: []item{itemString},
					},
				},
			},
			want: itemString,
		},
		{
			name: "empty allOf becomes single-element and is unwrapped",
			item: &itemAllOf{
				elems: []item{
					&itemAllOf{
						elems: []item{itemString},
					},
				},
			},
			want: itemString,
		},
		{
			name: "complex nested structure with mixed types",
			item: &itemAllOf{
				elems: []item{
					&itemAllOf{
						elems: []item{
							itemString,
							&itemAllOf{
								elems: []item{itemNumber},
							},
						},
					},
					&itemNot{
						elem: &itemAllOf{
							elems: []item{
								itemBool,
								&itemAllOf{
									elems: []item{&itemFormat{format: "date"}},
								},
							},
						},
					},
					itemString, // Duplicate, should be removed
				},
			},
			want: &itemAllOf{
				elems: []item{
					itemString,
					itemNumber,
					&itemNot{
						elem: &itemAllOf{
							elems: []item{
								itemBool,
								&itemFormat{format: "date"},
							},
						},
					},
				},
			},
		},
	}

	// Define comparison options for unexported fields
	cmpOpt := cmp.AllowUnexported(
		itemAllOf{},
		itemAnyOf{},
		itemFormat{},
		itemNot{},
		itemType{},
		property{},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAllOf(tt.item)
			qt.Assert(t, qt.CmpEquals(got, tt.want, cmpOpt))
		})
	}
}
