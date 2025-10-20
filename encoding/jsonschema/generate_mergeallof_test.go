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
)

func TestMergeAllOf(t *testing.T) {
	u := newUniqueItems()
	itemString := u.intern(&itemType{kinds: []string{"string"}})
	itemNumber := u.intern(&itemType{kinds: []string{"number"}})
	itemBool := u.intern(&itemType{kinds: []string{"boolean"}})

	tests := []struct {
		name string
		item internItem
		want internItem
	}{
		{
			name: "NonAllOfItemReturnsAsIs",
			item: itemString,
			want: itemString,
		},
		{
			name: "AllOfWithSingleElementReturnsThatElement",
			item: u.intern(&itemAllOf{
				elems: []internItem{itemString},
			}),
			want: itemString,
		},
		{
			name: "AllOfWithMultipleElementsStaysAsAllOf",
			item: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber},
			}),
		},
		{
			name: "NestedAllOfIsFlattened",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					itemString,
					u.intern(&itemAllOf{
						elems: []internItem{itemNumber, itemBool},
					}),
				},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber, itemBool},
			}),
		},
		{
			name: "MultipleNestedAllOfAreAllFlattened",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{itemString},
					}),
					u.intern(&itemAllOf{
						elems: []internItem{itemNumber},
					}),
					u.intern(&itemAllOf{
						elems: []internItem{itemBool},
					}),
				},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber, itemBool},
			}),
		},
		{
			name: "DeeplyNestedAllOfIsFullyFlattened",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					itemString,
					u.intern(&itemAllOf{
						elems: []internItem{
							itemNumber,
							u.intern(&itemAllOf{
								elems: []internItem{itemBool},
							}),
						},
					}),
				},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber, itemBool},
			}),
		},
		{
			name: "DuplicateItemsAreRemoved",
			item: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemString, itemNumber, itemString},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber},
			}),
		},
		{
			name: "DuplicateItemsAfterFlatteningAreRemoved",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					itemString,
					u.intern(&itemAllOf{
						elems: []internItem{itemString, itemNumber},
					}),
					itemString,
				},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{itemString, itemNumber},
			}),
		},
		{
			name: "AllOfNestedInOtherItemTypesHasChildrenMerged",
			item: u.intern(&itemNot{
				elem: u.intern(&itemAllOf{
					elems: []internItem{
						u.intern(&itemAllOf{
							elems: []internItem{
								u.intern(&itemAllOf{
									elems: []internItem{itemString},
								}),
								itemNumber,
							},
						}),
					},
				}),
			}),
			want: u.intern(&itemNot{
				elem: u.intern(&itemAllOf{
					elems: []internItem{itemString, itemNumber},
				}),
			}),
		},
		{
			name: "AllOfNestedInAnyOfIsRecursivelyMerged",
			item: u.intern(&itemAnyOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{
							u.intern(&itemAllOf{
								elems: []internItem{itemString},
							}),
							itemNumber,
						},
					}),
					itemBool,
				},
			}),
			want: u.intern(&itemAnyOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{itemString, itemNumber},
					}),
					itemBool,
				},
			}),
		},
		{
			name: "SingleElementAfterFlatteningAndDeduplication",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{itemString},
					}),
					u.intern(&itemAllOf{
						elems: []internItem{itemString},
					}),
				},
			}),
			want: itemString,
		},
		{
			name: "EmptyAllOfBecomesSingleElementAndIsUnwrapped",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{itemString},
					}),
				},
			}),
			want: itemString,
		},
		{
			name: "ComplexNestedStructureWithMixedTypes",
			item: u.intern(&itemAllOf{
				elems: []internItem{
					u.intern(&itemAllOf{
						elems: []internItem{
							itemString,
							u.intern(&itemAllOf{
								elems: []internItem{itemNumber},
							}),
						},
					}),
					u.intern(&itemNot{
						elem: u.intern(&itemAllOf{
							elems: []internItem{
								itemBool,
								u.intern(&itemAllOf{
									elems: []internItem{u.intern(&itemFormat{format: "date"})},
								}),
							},
						}),
					}),
					itemString, // Duplicate, should be removed
				},
			}),
			want: u.intern(&itemAllOf{
				elems: []internItem{
					itemString,
					itemNumber,
					u.intern(&itemNot{
						elem: u.intern(&itemAllOf{
							elems: []internItem{
								itemBool,
								u.intern(&itemFormat{format: "date"}),
							},
						}),
					}),
				},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAllOf(tt.item, u)
			qt.Assert(t, qt.Equals(got, tt.want))
		})
	}
}
