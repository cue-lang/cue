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

package cuetxtar

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cuetest"
	"github.com/google/go-cmp/cmp"
	"github.com/rogpeppe/go-internal/txtar"
)

// A TxTarTest represents a test run that process all CUE tests in the txtar
// format rooted in a given directory.
type TxTarTest struct {
	// Run TxTarTest on this directory.
	Root string

	// Name is a unique name for this test. The golden file for this test is
	// derived from the out/<name> file in the .txtar file.
	//
	// TODO: by default derive from the current base directory name.
	Name string

	// If Update is true, TestTxTar will update the out/Name file if it differs
	// from the original input. The user must set the output in Gold for this
	// to be detected.
	Update bool

	// Skip is a map of tests to skip to their skip message.
	Skip map[string]string

	// ToDo is a map of tests that should be skipped now, but should be fixed.
	ToDo map[string]string
}

// A Test represents a single test based on a .txtar file.
//
// A Test embeds *testing.T and should be used to report errors.
//
// A Test also embeds a *bytes.Buffer which is used to report test results,
// which are compared against the golden file for the test in the TxTar archive.
// If the test fails and the update flag is set to true, the Archive will be
// updated and written to disk.
type Test struct {
	// Allow Test to be used as a T.
	*testing.T

	prefix   string
	buf      *bytes.Buffer // the default buffer
	outFiles []file

	Archive *txtar.Archive

	// The absolute path of the current test directory.
	Dir string

	hasGold bool
}

func (t *Test) Write(b []byte) (n int, err error) {
	if t.buf == nil {
		t.buf = &bytes.Buffer{}
		t.outFiles = append(t.outFiles, file{t.prefix, t.buf})
	}
	return t.buf.Write(b)
}

type file struct {
	name string
	buf  *bytes.Buffer
}

func (t *Test) HasTag(key string) bool {
	prefix := []byte("#" + key)
	s := bufio.NewScanner(bytes.NewReader(t.Archive.Comment))
	for s.Scan() {
		b := s.Bytes()
		if bytes.Equal(bytes.TrimSpace(b), prefix) {
			return true
		}
	}
	return false
}

func (t *Test) Value(key string) (value string, ok bool) {
	prefix := []byte("#" + key + ":")
	s := bufio.NewScanner(bytes.NewReader(t.Archive.Comment))
	for s.Scan() {
		b := s.Bytes()
		if bytes.HasPrefix(b, prefix) {
			return string(bytes.TrimSpace(b[len(prefix):])), true
		}
	}
	return "", false
}

// Bool searches for a line starting with #key: value in the comment and
// returns true if the key exists and the value is true.
func (t *Test) Bool(key string) bool {
	s, ok := t.Value(key)
	return ok && s == "true"
}

// Rel converts filename to a normalized form so that it will given the same
// output across different runs and OSes.
func (t *Test) Rel(filename string) string {
	rel, err := filepath.Rel(t.Dir, filename)
	if err != nil {
		return filepath.Base(filename)
	}
	return filepath.ToSlash(rel)
}

// WriteErrors writes strings and
func (t *Test) WriteErrors(err errors.Error) {
	if err != nil {
		errors.Print(t, err, &errors.Config{
			Cwd:     t.Dir,
			ToSlash: true,
		})
	}
}

// Write file in a directory.
func (t *Test) WriteFile(f *ast.File) {
	// TODO: use FileWriter instead in separate CL.
	fmt.Fprintln(t, "==", filepath.Base(f.Filename))
	_, _ = t.Write(formatNode(t.T, f))
}

// Writer returns a Writer with the given name.
func (t *Test) Writer(name string) io.Writer {
	switch name {
	case "":
		name = t.prefix
	default:
		name = path.Join(t.prefix, name)
	}

	for _, f := range t.outFiles {
		if f.name == name {
			return f.buf
		}
	}

	w := &bytes.Buffer{}
	t.outFiles = append(t.outFiles, file{name, w})

	if name == t.prefix {
		t.buf = w
	}

	return w
}

func formatNode(t *testing.T, n ast.Node) []byte {
	t.Helper()

	b, err := format.Node(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ValidInstances returns the valid instances for this .txtar file or skips the
// test if there is an error loading the instances.
func (t *Test) ValidInstances(args ...string) []*build.Instance {
	a := t.RawInstances(args...)
	for _, i := range a {
		if i.Err != nil {
			if t.hasGold {
				t.Fatal("Parse error: ", i.Err)
			}
			t.Skip("Parse error: ", i.Err)
		}
	}
	return a
}

// RawInstances returns the intstances represented by this .txtar file. The
// returned instances are not checked for errors.
func (t *Test) RawInstances(args ...string) []*build.Instance {
	return Load(t.Archive, t.Dir, args...)
}

// Load loads the intstances of a txtar file. By default, it only loads
// files in the root directory. Relative files in the archive are given an
// absolution location by prefixing it with dir.
func Load(a *txtar.Archive, dir string, args ...string) []*build.Instance {
	auto := len(args) == 0
	overlay := map[string]load.Source{}
	for _, f := range a.Files {
		if auto && !strings.Contains(f.Name, "/") {
			args = append(args, f.Name)
		}
		overlay[filepath.Join(dir, f.Name)] = load.FromBytes(f.Data)
	}

	cfg := &load.Config{
		Dir:     dir,
		Overlay: overlay,
	}

	return load.Instances(args, cfg)
}

// Run runs tests defined in txtar files in root or its subdirectories.
// Only tests for which an `old/name` test output file exists are run.
func (x *TxTarTest) Run(t *testing.T, f func(tc *Test)) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	root := x.Root

	err = filepath.Walk(root, func(fullpath string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}

		if info.IsDir() || filepath.Ext(fullpath) != ".txtar" {
			return nil
		}

		str := filepath.ToSlash(fullpath)
		p := strings.Index(str, "/testdata/")
		testName := str[p+len("/testdata/") : len(str)-len(".txtar")]

		t.Run(testName, func(t *testing.T) {
			a, err := txtar.ParseFile(fullpath)
			if err != nil {
				t.Fatalf("error parsing txtar file: %v", err)
			}

			tc := &Test{
				T:       t,
				Archive: a,
				Dir:     filepath.Dir(filepath.Join(dir, fullpath)),

				prefix: path.Join("out", x.Name),
			}

			if tc.HasTag("skip") {
				t.Skip()
			}

			if msg, ok := x.Skip[testName]; ok {
				t.Skip(msg)
			}
			if msg, ok := x.ToDo[testName]; ok {
				t.Skip(msg)
			}

			update := false

			for i, f := range a.Files {

				// TODO: not entirely correct.
				if strings.HasPrefix(f.Name, tc.prefix) {
					tc.hasGold = true
				}

				// Format CUE files as required
				if tc.HasTag("noformat") || !strings.HasSuffix(f.Name, ".cue") {
					continue
				}
				if ff, err := format.Source(f.Data); err == nil {
					if bytes.Equal(f.Data, ff) {
						continue
					}
					if cuetest.FormatTxtar {
						update = true
						a.Files[i].Data = ff
					}
				}
			}

			f(tc)

			for _, sub := range tc.outFiles {
				var gold *txtar.File
				for i, f := range a.Files {
					if f.Name == sub.name {
						gold = &a.Files[i]
					}
				}

				result := sub.buf.Bytes()

				switch {
				case gold == nil:
					a.Files = append(a.Files, txtar.File{Name: sub.name})
					gold = &a.Files[len(a.Files)-1]

				case bytes.Equal(gold.Data, result):
					continue
				}

				if x.Update || cuetest.UpdateGoldenFiles {
					update = true
					gold.Data = result
					continue
				}

				t.Errorf("result for %s differs:\n%s",
					sub.name,
					cmp.Diff(string(gold.Data), string(result)))
			}

			if update {
				err = ioutil.WriteFile(fullpath, txtar.Format(a), 0644)
				if err != nil {
					t.Fatal(err)
				}
			}
		})

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
}
