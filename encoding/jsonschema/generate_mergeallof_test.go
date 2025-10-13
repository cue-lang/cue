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
			name: "NonAllOfItemReturnsAsIs",
			item: itemString,
			want: itemString,
		},
		{
			name: "AllOfWithSingleElementReturnsThatElement",
			item: &itemAllOf{
				elems: []item{itemString},
			},
			want: itemString,
		},
		{
			name: "AllOfWithMultipleElementsStaysAsAllOf",
			item: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
		},
		{
			name: "NestedAllOfIsFlattened",
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
			name: "MultipleNestedAllOfAreAllFlattened",
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
			name: "DeeplyNestedAllOfIsFullyFlattened",
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
			name: "DuplicateItemsAreRemoved",
			item: &itemAllOf{
				elems: []item{itemString, itemString, itemNumber, itemString},
			},
			want: &itemAllOf{
				elems: []item{itemString, itemNumber},
			},
		},
		{
			name: "DuplicateItemsAfterFlatteningAreRemoved",
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
			name: "AllOfNestedInOtherItemTypesHasChildrenMerged",
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
			name: "AllOfNestedInAnyOfIsRecursivelyMerged",
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
			name: "SingleElementAfterFlatteningAndDeduplication",
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
			name: "EmptyAllOfBecomesSingleElementAndIsUnwrapped",
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
			name: "ComplexNestedStructureWithMixedTypes",
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
