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

package filetypes

import (
	"strings"

	"cuelang.org/go/cue/ast"
)

// IsPackage reports whether a command-line argument is a package based on its
// lexical representation alone.
func IsPackage(s string) bool {
	switch s {
	case ".", "..":
		return true
	case "", "-":
		return false
	}

	ip := ast.ParseImportPath(s)
	if ip.ExplicitQualifier {
		if !ast.IsValidIdent(ip.Qualifier) || strings.Contains(ip.Path, ":") || ip.Path == "-" {
			// TODO potentially widen the scope of "file-like"
			// paths here to include more invalid package paths?
			return false
		}
		// If it's got an explicit qualifier, the path has a colon in
		// which isn't generally allowed in CUE file names.
		return true
	}
	if ip.Version != "" {
		if strings.Contains(ip.Version, "/") {
			// We'll definitely not allow slashes in the version string
			// so treat it as a file name.
			return false
		}
		// Looks like an explicit version suffix.
		// Deliberately leave the syntax fairly open so that
		// we get reasonable error messages when invalid version
		// queries are specified.
		return true
	}

	// No version and no qualifier.
	// Assuming we terminate search for packages once a scoped qualifier is
	// found, we know that any file without an extension (except maybe '-')
	// is invalid. We can therefore assume it is a package.
	// The section may still contain a dot, for instance ./foo/., ./.foo/, or ./foo/...
	return strings.TrimLeft(fileExt(s), ".") == ""

	// NOTE/TODO: we have not needed to check whether it is an absolute package
	// or whether the package starts with a dot. Potentially we could thus relax
	// the requirement that packages be dots if it is clear that the package
	// name will not interfere with command names in all circumstances.
}
