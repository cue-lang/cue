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

package http

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
)

func TestParseHeaders(t *testing.T) {
	req := `
	header: {
		"Accept-Language": "en,nl"
	}
	trailer: {
		"Accept-Language": "en,nl"
		User: "foo"
	}
	`
	testCases := []struct {
		req   string
		field string
		out   string
	}{{
		field: "header",
		out:   "nil",
	}, {
		req:   req,
		field: "non-exist",
		out:   "nil",
	}, {
		req:   req,
		field: "header",
		out:   "Accept-Language: en,nl\r\n",
	}, {
		req:   req,
		field: "trailer",
		out:   "Accept-Language: en,nl\r\nUser: foo\r\n",
	}, {
		req: `
		header: {
			"1": 'a'
		}
		`,
		field: "header",
		out:   "header.\"1\": cannot use value 'a' (type bytes) as string",
	}, {
		req: `
			header: 1
			`,
		field: "header",
		out:   "header: cannot use value 1 (type int) as struct",
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := cue.Runtime{}
			inst, err := r.Compile("http headers", tc.req)
			if err != nil {
				t.Fatal(err)
			}

			h, err := parseHeaders(inst.Value(), tc.field)

			b := &strings.Builder{}
			switch {
			case err != nil:
				fmt.Fprint(b, err)
			case h == nil:
				b.WriteString("nil")
			default:
				_ = h.Write(b)
			}

			got := b.String()
			if got != tc.out {
				t.Errorf("got %q; want %q", got, tc.out)
			}
		})
	}
}
