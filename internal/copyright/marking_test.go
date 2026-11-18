// Copyright 2026 CUE Authors
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

package copyright

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The tag that names a file's license. It is spelled in two parts so
// that no tool reads this file's own license from the line.
const licenseTag = "SPDX-License-" + "Identifier:"

// The licenses of the files derived from other projects.
const (
	apache = "Apache-2.0"
	bsd    = "BSD-3-Clause"
)

// goLicenseSum is the SHA-256 of the license of Go and of its
// golang.org/x repositories, as the Go 1.26 toolchain carries it in
// its LICENSE file. LICENSES/BSD-3-Clause.txt is that text.
const goLicenseSum = "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"

// skipDirs are the directories, by path from the module's root, which
// hold nothing of the public source: the checks are about what the
// module publishes. A directory whose name starts with a dot, a vendor
// directory, and a nested module are skipped wherever they are.
var skipDirs = []string{
	"_scripts",
	"ee",
	"internal/_e2e",
	"internal/ci",
	"private",
	"specs",
	// The license texts themselves.
	"LICENSES",
}

// quoted are the files which carry another holder's notice as data,
// not as their own: each is a test archive holding a sample file.
var quoted = map[string]string{
	"cmd/cue/cmd/testdata/script/def_proto.txtar":         "holds Google's descriptor.proto, with its license text",
	"cmd/cue/cmd/testdata/script/import_proto.txtar":      "holds Google's descriptor.proto, with its license text",
	"cmd/cue/cmd/testdata/script/import_proto2.txtar":     "holds Google's descriptor.proto, with its license text",
	"internal/pretty/testdata/package-blank-before.txtar": "its ACME notice is sample input for the printer",
}

var (
	// noticeLine matches a copyright notice in a line comment, and
	// captures the holder with whatever follows the word.
	noticeLine = regexp.MustCompile(`(?m)^[ \t]*(?://|#)[ \t]*(?:Copyright\b|SPDX-FileCopyrightText:)(.*)$`)
	tagLine    = regexp.MustCompile(`(?m)^[ \t]*(?://|#)[ \t]*` + licenseTag + `[ \t]*(.*?)[ \t]*$`)
)

// headSize is how much of a file is read for its license line, which
// is how much the tools that classify the repository's files read.
const headSize = 4096

// marked is what a file says about itself.
type marked struct {
	cue, golang, other bool   // the copyright holders its notices name
	tagged             bool   // whether its head has a license line
	license            string // the expression of that line
}

func (m marked) licenses() []string {
	var ids []string
	for _, f := range strings.Fields(m.license) {
		if f != "AND" && f != "OR" {
			ids = append(ids, f)
		}
	}
	return ids
}

// goDerived reports whether the file holds material of Go's, under
// Go's license.
func (m marked) goDerived() bool {
	return m.golang && slices.Contains(m.licenses(), bsd)
}

func readMarking(data []byte) marked {
	var m marked
	for _, match := range noticeLine.FindAllSubmatch(data, -1) {
		switch holder := string(match[1]); {
		case strings.Contains(holder, "CUE Authors"):
			m.cue = true
		case strings.Contains(holder, "The Go Authors"):
			m.golang = true
		default:
			m.other = true
		}
	}
	head := data[:min(len(data), headSize)]
	if match := tagLine.FindSubmatch(head); match != nil {
		m.tagged = true
		m.license = string(match[1])
	}
	return m
}

// TestThirdPartyFilesMarked checks how the module's public source
// records the material it holds of other projects.
//
// A file with another copyright holder's notice names its license in a
// license line at its head, beside that notice: Go's license for a file
// of the Go Authors alone, and both licenses, joined by AND, for a file
// which also holds the CUE Authors' own. The text of each license named
// is under LICENSES. A directory holding a file derived from Go carries
// Go's license as its LICENSE file as well, and no other directory
// carries one: that file is where tools which work by package, rather
// than by file, find a package's license.
//
// The check cannot find material copied without its notice; that is
// for review.
func TestThirdPartyFilesMarked(t *testing.T) {
	root := filepath.Join("..", "..")
	if mod, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || !bytes.HasPrefix(mod, []byte("module cuelang.org/go\n")) {
		t.Fatalf("%s is not the root of the module: %v", root, err)
	}
	read := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	goLicense, err := os.ReadFile(filepath.Join(root, "LICENSES", bsd+".txt"))
	if err != nil {
		t.Errorf("the text of %s, which the files derived from Go are under: %v", bsd, err)
	} else if sum := fmt.Sprintf("%x", sha256.Sum256(goLicense)); sum != goLicenseSum {
		t.Errorf("LICENSES/%s.txt is not Go's license text: its SHA-256 is %s, want %s", bsd, sum, goLicenseSum)
	}

	goDirs := make(map[string]bool)   // directories holding a file derived from Go
	licensed := make(map[string]bool) // directories with a LICENSE file
	var failures []string
	fail := func(rel, format string, args ...any) {
		failures = append(failures, rel+": "+fmt.Sprintf(format, args...))
	}
	err = filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || slices.Contains(skipDirs, rel) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(file, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		dir := path.Dir(rel)
		if d.Name() == "LICENSE" {
			// The root's is the license of the module.
			if dir != "." {
				licensed[dir] = true
			}
			return nil
		}
		if _, ok := quoted[rel]; ok {
			return nil
		}
		m := readMarking(read(rel))
		ids := m.licenses()
		switch {
		case m.golang && !slices.Contains(ids, bsd):
			fail(rel, "has a Go Authors notice, but no license line naming %s in its first %d bytes", bsd, headSize)
		case m.golang && m.cue && m.license != apache+" AND "+bsd:
			fail(rel, "has a CUE Authors and a Go Authors notice, so its license line must be %q, not %q", apache+" AND "+bsd, m.license)
		case m.other && !m.tagged:
			fail(rel, "has another copyright holder's notice, but no license line in its first %d bytes", headSize)
		}
		for _, id := range ids {
			if _, err := os.Stat(filepath.Join(root, "LICENSES", id+".txt")); err != nil {
				fail(rel, "names license %s, whose text is not under LICENSES", id)
			}
		}
		if m.goDerived() {
			goDirs[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for dir := range goDirs {
		switch {
		case !licensed[dir]:
			fail(dir, "holds a file derived from Go, but no LICENSE file with Go's license")
		case !bytes.Equal(read(dir+"/LICENSE"), goLicense):
			fail(dir+"/LICENSE", "is not Go's license, the text of LICENSES/%s.txt", bsd)
		}
	}
	for dir := range licensed {
		if !goDirs[dir] {
			fail(dir+"/LICENSE", "is in a directory which holds no file derived from Go")
		}
	}
	slices.Sort(failures)
	for _, f := range failures {
		t.Error(f)
	}
	if len(failures) > 0 {
		t.Logf("%d findings", len(failures))
	}
}

func TestReadMarking(t *testing.T) {
	tag := "// " + licenseTag + " "
	tests := []struct {
		name string
		file string
		want marked
	}{
		{"CUE's own", "// Copyright 2026 CUE Authors\n\npackage p\n", marked{cue: true}},
		{"Go's", tag + bsd + "\n// Copyright 2009 The Go Authors. All rights reserved.\n",
			marked{golang: true, tagged: true, license: bsd}},
		{"both", tag + apache + " AND " + bsd + "\n// Copyright 2020 CUE Authors\n\n// Copyright 2009 The Go Authors. All rights reserved.\n",
			marked{cue: true, golang: true, tagged: true, license: apache + " AND " + bsd}},
		{"a notice without a year", "// Copyright 2026 The CUE Authors\n// Copyright The Go Authors.\n", marked{cue: true, golang: true}},
		{"a notice as a tag", "# SPDX-FileCopyrightText: Someone Else\n", marked{other: true}},
		{"another holder", "// Copyright 2017 Istio Authors\n", marked{other: true}},
		{"a holder whose name holds Go's", "// Copyright (c) 2013, The GoGo Authors. All rights reserved.\n", marked{other: true}},
		{"a notice in code is not one", "var s = \"Copyright 2009 The Go Authors\"\n", marked{}},
		{"a license line past the head does not count", strings.Repeat("//\n", headSize) + tag + bsd + "\n", marked{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := readMarking([]byte(tc.file)); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
