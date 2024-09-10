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
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
	"github.com/google/go-cmp/cmp"
	"github.com/rogpeppe/go-internal/diff"
	"golang.org/x/tools/txtar"
)

// A TxTarTest represents a test run that process all CUE tests in the txtar
// format rooted in a given directory. See the [Test] documentation for
// more details.
type TxTarTest struct {
	// Run TxTarTest on this directory.
	Root string

	// Name is a unique name for this test. The golden file for this test is
	// derived from the out/<name> file in the .txtar file.
	//
	// TODO: by default derive from the current base directory name.
	Name string

	// Fallback allows the golden tests named by Fallback to pass tests in
	// case the golden file corresponding to Name does not exist.
	// The feature can be used to have two implementations of the same
	// functionality share the same test sets.
	Fallback string

	// Skip is a map of tests to skip; the key is the test name; the value is the
	// skip message.
	Skip map[string]string

	// ToDo is a map of tests that should be skipped now, but should be fixed.
	ToDo map[string]string

	// LoadConfig is passed to load.Instances when loading instances.
	// It's copied before doing that and the Dir and Overlay fields are overwritten.
	LoadConfig load.Config

	// If Matrix is non-nil, the tests are run for each configuration in the
	// matrix.
	Matrix cuetdtest.Matrix

	// DebugArchive, if set, is loaded instead of the on-disk archive. This allows
	// a test to be used for debugging.
	DebugArchive string
}

// A Test represents a single test based on a .txtar file.
//
// A Test embeds *[testing.T] and should be used to report errors.
//
// Entries within the txtar file define CUE files (available via the
// Instances and RawInstances methods) and expected output
// (or "golden") files (names starting with "out/\(testname)"). The "main" golden
// file is "out/\(testname)" itself, used when [Test] is used directly as an [io.Writer]
// and with [Test.WriteFile].
//
// When the test function has returned, output written with [Test.Write], [Test.Writer]
// and friends is checked against the expected output files.
//
// A txtar file can define test-specific tags and values in the comment section.
// These are available via the [Test.HasTag] and [Test.Value] methods.
// The #skip tag causes a [Test] to be skipped.
// The #noformat tag causes the $CUE_FORMAT_TXTAR value
// to be ignored.
//
// If the output differs and $CUE_UPDATE is non-empty, the txtar file will be
// updated and written to disk with the actual output data replacing the
// out files.
//
// If $CUE_FORMAT_TXTAR is non-empty, any CUE files in the txtar
// file will be updated to be properly formatted, unless the #noformat
// tag is present.
type Test struct {
	// Allow Test to be used as a T.
	*testing.T
	*cuetdtest.M

	prefix   string
	fallback string
	buf      *bytes.Buffer // the default buffer
	outFiles []file

	Archive    *txtar.Archive
	LoadConfig load.Config

	// The absolute path of the current test directory.
	Dir string

	hasGold bool
}

// Ensure that Test always implements testing.TB.
// Note that testing.TB may gain new methods in future Go releases.
var _ testing.TB = (*Test)(nil)

// Write implements [io.Writer] by writing to the output for the test,
// which will be tested against the main golden file.
func (t *Test) Write(b []byte) (n int, err error) {
	if t.buf == nil {
		t.buf = &bytes.Buffer{}
		t.outFiles = append(t.outFiles, file{t.prefix, t.fallback, t.buf, false})
	}
	return t.buf.Write(b)
}

type file struct {
	name     string
	fallback string
	buf      *bytes.Buffer
	diff     bool // true if this contains a diff between fallback and main
}

// HasTag reports whether the tag with the given key is defined
// for the current test. A tag x is defined by a line in the comment
// section of the txtar file like:
//
//	#x
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

// Value returns the value for the given key for this test and
// reports whether it was defined.
//
// A value is defined by a line in the comment section of the txtar
// file like:
//
//	#key: value
//
// White space is trimmed from the value before returning.
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
// reports whether the key exists and its value is true.
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

// WriteErrors writes the full list of errors in err to the test output.
func (t *Test) WriteErrors(err errors.Error) {
	if err != nil {
		errors.Print(t, err, &errors.Config{
			Cwd:     t.Dir,
			ToSlash: true,
		})
	}
}

// WriteFile formats f and writes it to the main output,
// prefixed by a line of the form:
//
//	== name
//
// where name is the base name of f.Filename.
func (t *Test) WriteFile(f *ast.File) {
	// TODO: use FileWriter instead in separate CL.
	fmt.Fprintln(t, "==", filepath.Base(f.Filename))
	_, _ = t.Write(formatNode(t.T, f))
}

// Writer returns a Writer with the given name. Data written will
// be checked against the file with name "out/\(testName)/\(name)"
// in the txtar file. If name is empty, data will be written to the test
// output and checked against "out/\(testName)".
func (t *Test) Writer(name string) io.Writer {
	var fallback string
	switch name {
	case "":
		name = t.prefix
		fallback = t.fallback
	default:
		fallback = path.Join(t.fallback, name)
		name = path.Join(t.prefix, name)
	}

	for _, f := range t.outFiles {
		if f.name == name {
			return f.buf
		}
	}

	w := &bytes.Buffer{}
	t.outFiles = append(t.outFiles, file{name, fallback, w, false})

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

// Instance returns the single instance representing the
// root directory in the txtar file.
func (t *Test) Instance() *build.Instance {
	t.Helper()
	return t.Instances()[0]
}

// Instances returns the valid instances for this .txtar file or skips the
// test if there is an error loading the instances.
func (t *Test) Instances(args ...string) []*build.Instance {
	t.Helper()

	a := t.RawInstances(args...)
	for _, i := range a {
		if i.Err != nil {
			if t.hasGold {
				t.Fatal("Parse error: ", errors.Details(i.Err, nil))
			}
			t.Skip("Parse error: ", errors.Details(i.Err, nil))
		}
	}
	return a
}

// RawInstances returns the intstances represented by this .txtar file. The
// returned instances are not checked for errors.
func (t *Test) RawInstances(args ...string) []*build.Instance {
	return loadWithConfig(t.Archive, t.Dir, t.LoadConfig, args...)
}

// Load loads the intstances of a txtar file. By default, it only loads
// files in the root directory. Relative files in the archive are given an
// absolute location by prefixing it with dir.
func Load(a *txtar.Archive, dir string, args ...string) []*build.Instance {
	// Don't let Env be nil, as the tests shouldn't depend on os.Environ.
	return loadWithConfig(a, dir, load.Config{Env: []string{}}, args...)
}

func loadWithConfig(a *txtar.Archive, dir string, cfg load.Config, args ...string) []*build.Instance {
	auto := len(args) == 0
	overlay := map[string]load.Source{}
	for _, f := range a.Files {
		if auto && !strings.Contains(f.Name, "/") {
			args = append(args, f.Name)
		}
		overlay[filepath.Join(dir, f.Name)] = load.FromBytes(f.Data)
	}

	cfg.Dir = dir
	cfg.Overlay = overlay

	return load.Instances(args, &cfg)
}

// Run runs tests defined in txtar files in x.Root or its subdirectories.
//
// The function f is called for each such txtar file. See the [Test] documentation
// for more details.
func (x *TxTarTest) Run(t *testing.T, f func(tc *Test)) {
	if x.Matrix == nil {
		x.run(t, nil, f)
		return
	}
	x.Matrix.Do(t, func(t *testing.T, m *cuetdtest.M) {
		test := *x
		if s := m.Fallback(); s != "" {
			test.Fallback = test.Name
			if s != cuetdtest.DefaultVersion {
				test.Fallback += "-" + s
			}
		}
		if s := m.Name(); s != cuetdtest.DefaultVersion {
			test.Name += "-" + s
		}
		test.run(t, m, func(tc *Test) {
			f(tc)
		})
	})
}

// Runtime returns a new runtime based on the configuration of the test.
func (t *Test) Runtime() *runtime.Runtime {
	return (*runtime.Runtime)(t.CueContext())
}

// CueContext returns a new cue.CueContext based on the configuration of the test.
func (t *Test) CueContext() *cue.Context {
	if t.M != nil {
		return t.M.CueContext()
	}
	return cuecontext.New()
}

func (x *TxTarTest) run(t *testing.T, m *cuetdtest.M, f func(tc *Test)) {
	t.Helper()

	if x.DebugArchive != "" {
		archive := txtar.Parse([]byte(x.DebugArchive))

		t.Run("", func(t *testing.T) {
			if len(archive.Files) == 0 {
				t.Fatal("DebugArchive contained no files")
			}
			tc := &Test{
				T:       t,
				M:       m,
				Archive: archive,
				Dir:     "/tmp",

				prefix:     path.Join("out", x.Name),
				LoadConfig: x.LoadConfig,
			}
			// Don't let Env be nil, as the tests shouldn't depend on os.Environ.
			if tc.LoadConfig.Env == nil {
				tc.LoadConfig.Env = []string{}
			}

			f(tc)

			// Unconditionally log the output and fail.
			t.Log(tc.buf.String())
			t.Error("DebugArchive tests always fail")
		})
		return
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	root := x.Root

	err = filepath.WalkDir(root, func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(fullpath) != ".txtar" {
			return nil
		}

		str := filepath.ToSlash(fullpath)
		p := strings.Index(str, "/testdata/")
		var testName string
		// Do not include the name of the test if the Matrix feature is not used
		// to ensure that the todo lists of existing tests do not break.
		if x.Matrix != nil && x.Name != "" {
			testName = x.Name + "/"
		}
		testName += str[p+len("/testdata/") : len(str)-len(".txtar")]

		t.Run(testName, func(t *testing.T) {
			a, err := txtar.ParseFile(fullpath)
			if err != nil {
				t.Fatalf("error parsing txtar file: %v", err)
			}

			tc := &Test{
				T:       t,
				M:       m,
				Archive: a,
				Dir:     filepath.Dir(filepath.Join(dir, fullpath)),

				prefix:     path.Join("out", x.Name),
				LoadConfig: x.LoadConfig,
			}
			// Don't let Env be nil, as the tests shouldn't depend on os.Environ.
			if tc.LoadConfig.Env == nil {
				tc.LoadConfig.Env = []string{}
			}
			if x.Fallback != "" {
				tc.fallback = path.Join("out", x.Fallback)
			} else {
				tc.fallback = tc.prefix
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
				hasPrefix := func(s string) bool {
					// It's either "\(tc.prefix)" or "\(tc.prefix)/..." but not some other name
					// that happens to start with tc.prefix.
					return strings.HasPrefix(f.Name, s) && (f.Name == s || f.Name[len(s)] == '/')
				}

				tc.hasGold = hasPrefix(tc.prefix) || hasPrefix(tc.fallback)

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

			// Track the position of the fallback files.
			index := make(map[string]int, len(a.Files))
			for i, f := range a.Files {
				if _, ok := index[f.Name]; ok {
					t.Errorf("duplicated txtar file entry %s", f.Name)
				}
				index[f.Name] = i
			}

			// Record ordering of files in the archive to preserve that ordering
			// later.
			ordering := maps.Clone(index)

			// Add diff files between fallback and main file. These are added
			// as regular output files so that they can be updated as well.
			for _, sub := range tc.outFiles {
				if sub.fallback == sub.name {
					continue
				}
				if j, ok := index[sub.fallback]; ok {
					if _, ok := ordering[sub.name]; !ok {
						ordering[sub.name] = j
					}
					fallback := a.Files[j].Data

					result := sub.buf.Bytes()
					if len(result) == 0 || len(fallback) == 0 {
						continue
					}

					diffName := "diff/-" + sub.name + "<==>+" + sub.fallback
					if _, ok := ordering[diffName]; !ok {
						ordering[diffName] = j
					}
					switch diff := diff.Diff("old", fallback, "new", result); {
					case len(diff) > 0:
						tc.outFiles = append(tc.outFiles, file{
							name: diffName,
							buf:  bytes.NewBuffer(diff),
							diff: true,
						})

					default:
						// Only update file if anything changes.
						if _, ok := index[sub.name]; ok {
							delete(index, sub.name)
							if !cuetest.UpdateGoldenFiles {
								t.Errorf("file %q exists but is equal to fallback", sub.name)
							}
							update = cuetest.UpdateGoldenFiles
						}
						if _, ok := index[diffName]; ok {
							delete(index, diffName)
							if !cuetest.UpdateGoldenFiles {
								t.Errorf("file %q exists but is empty", diffName)
							}
							update = cuetest.UpdateGoldenFiles
						}
						// Remove all diff-related todo files.
						for n := range index {
							if strings.HasPrefix(n, "diff/todo/") {
								delete(index, n)
								if !cuetest.UpdateGoldenFiles {
									t.Errorf("todo file %q exists without changes", n)
								}
								update = cuetest.UpdateGoldenFiles
							}
						}
					}
				}
			}

			files := make([]txtar.File, 0, len(a.Files))

			for _, sub := range tc.outFiles {
				result := sub.buf.Bytes()

				files = append(files, txtar.File{Name: sub.name})
				gold := &files[len(files)-1]

				if i, ok := index[sub.name]; ok {
					gold.Data = a.Files[i].Data
					delete(index, sub.name)

					if bytes.Equal(gold.Data, result) {
						continue
					}
				} else if i, ok := index[sub.fallback]; ok {
					gold.Data = a.Files[i].Data

					// Use the golden file of the fallback set if it matches.
					if bytes.Equal(gold.Data, result) {
						gold.Name = sub.fallback
						delete(index, sub.fallback)
						continue
					}
				}

				if cuetest.UpdateGoldenFiles {
					update = true
					gold.Data = result
					continue
				}

				// Skip the test if just the diff differs.
				// TODO: also fail once diffs are fully in use.
				if sub.diff {
					continue
				}

				t.Errorf("result for %s differs: (-want +got)\n%s",
					sub.name,
					cmp.Diff(string(gold.Data), string(result)),
				)
				t.Errorf("actual result: %q", result)
			}

			// Add remaining unrelated files, ignoring files that were already
			// added.
			for _, f := range a.Files {
				if _, ok := index[f.Name]; ok {
					files = append(files, f)
				}
			}
			a.Files = files

			if update {
				slices.SortStableFunc(a.Files, func(i, j txtar.File) int {
					p, ok := ordering[i.Name]
					if !ok {
						p = len(a.Files)
					}
					q, ok := ordering[j.Name]
					if !ok {
						q = len(a.Files)
					}
					return p - q
				})

				err = os.WriteFile(fullpath, txtar.Format(a), 0644)
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
