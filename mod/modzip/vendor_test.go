// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modzip

import "testing"

func TestIsVendoredPackage(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: "cue.mod/vendor/foo", want: true},
		{path: "vendor/foo/foo.go", want: false},
	} {
		got := isVendoredPackage(tc.path)
		if got != tc.want {
			t.Errorf("isVendoredPackage(%q) = %t; want %t", tc.path, got, tc.want)
		}
	}
}
