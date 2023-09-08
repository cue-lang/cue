// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/internal/mod/module"
	modzip "cuelang.org/go/internal/mod/zip"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/tools/txtar"
)

type testParams struct {
	path, version, wantErr, hash string
	want                         string
	archive                      *txtar.Archive
}

// readTest loads a test from a txtar file. The comment section of the file
// should contain lines with key=value pairs. Valid keys are the field names
// from testParams.
func readTest(file string) (testParams, error) {
	var test testParams
	var err error
	test.archive, err = txtar.ParseFile(file)
	if err != nil {
		return testParams{}, err
	}
	for i, f := range test.archive.Files {
		if f.Name == "want" {
			test.want = string(f.Data)
			test.archive.Files = append(test.archive.Files[:i], test.archive.Files[i+1:]...)
			break
		}
	}

	lines := strings.Split(string(test.archive.Comment), "\n")
	for n, line := range lines {
		n++ // report line numbers starting with 1
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return testParams{}, fmt.Errorf("%s:%d: missing = separator", file, n)
		}
		key, value := strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:])
		if strings.HasPrefix(value, "\"") {
			unq, err := strconv.Unquote(value)
			if err != nil {
				return testParams{}, fmt.Errorf("%s:%d: %v", file, n, err)
			}
			value = unq
		}
		switch key {
		case "path":
			test.path = value
		case "version":
			test.version = value
		case "wantErr":
			test.wantErr = value
		case "hash":
			test.hash = value
		default:
			return testParams{}, fmt.Errorf("%s:%d: unknown key %q", file, n, key)
		}
	}

	return test, nil
}

func extractTxtarToTempDir(t testing.TB, arc *txtar.Archive) (dir string, err error) {
	dir = t.TempDir()
	for _, f := range arc.Files {
		filePath := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0777); err != nil {
			return "", err
		}
		if err := os.WriteFile(filePath, f.Data, 0666); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func extractTxtarToTempZip(t *testing.T, arc *txtar.Archive) (zipPath string, err error) {
	zipPath = filepath.Join(t.TempDir(), "txtar.zip")

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := zipFile.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	zw := zip.NewWriter(zipFile)
	for _, f := range arc.Files {
		zf, err := zw.Create(f.Name)
		if err != nil {
			return "", err
		}
		if _, err := zf.Write(f.Data); err != nil {
			return "", err
		}
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return zipFile.Name(), nil
}

type fakeFileIO struct{}

func (fakeFileIO) Path(f fakeFile) string                { return f.name }
func (fakeFileIO) Lstat(f fakeFile) (os.FileInfo, error) { return fakeFileInfo{f}, nil }
func (fakeFileIO) Open(f fakeFile) (io.ReadCloser, error) {
	if f.data != nil {
		return io.NopCloser(bytes.NewReader(f.data)), nil
	}
	if f.size >= uint64(modzip.MaxZipFile<<1) {
		return nil, fmt.Errorf("cannot open fakeFile of size %d", f.size)
	}
	return io.NopCloser(io.LimitReader(zeroReader{}, int64(f.size))), nil
}

type fakeFile struct {
	name  string
	isDir bool
	size  uint64
	data  []byte // if nil, Open will access a sequence of 0-bytes
}

type fakeFileInfo struct {
	f fakeFile
}

func (fi fakeFileInfo) Name() string {
	return path.Base(fi.f.name)
}

func (fi fakeFileInfo) Size() int64 {
	if fi.f.size == 0 {
		return int64(len(fi.f.data))
	}
	return int64(fi.f.size)
}
func (fi fakeFileInfo) Mode() os.FileMode {
	if fi.f.isDir {
		return os.ModeDir | 0o755
	}
	return 0o644
}

func (fi fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi fakeFileInfo) IsDir() bool        { return fi.f.isDir }
func (fi fakeFileInfo) Sys() interface{}   { return nil }

type zeroReader struct{}

func (r zeroReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0
	}
	return len(b), nil
}

func formatCheckedFiles(cf modzip.CheckedFiles) string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "valid:\n")
	for _, f := range cf.Valid {
		fmt.Fprintln(buf, f)
	}
	fmt.Fprintf(buf, "\nomitted:\n")
	for _, f := range cf.Omitted {
		fmt.Fprintf(buf, "%s: %v\n", f.Path, f.Err)
	}
	fmt.Fprintf(buf, "\ninvalid:\n")
	for _, f := range cf.Invalid {
		fmt.Fprintf(buf, "%s: %v\n", f.Path, f.Err)
	}
	return buf.String()
}

func TestCheckFilesWithDirWithTrailingSlash(t *testing.T) {
	// When checking a zip file,
	files := []fakeFile{{
		name:  "cue.mod/",
		isDir: true,
	}, {
		name: "cue.mod/module.cue",
		data: []byte(`module: "example.com/m"`),
	}}
	_, err := modzip.CheckFiles[fakeFile](files, fakeFileIO{})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCheckFiles verifies behavior of CheckFiles. Note that CheckFiles is also
// covered by TestCreate, TestCreateDir, and TestCreateSizeLimits, so this test
// focuses on how multiple errors and omissions are reported, rather than trying
// to cover every case.
func TestCheckFiles(t *testing.T) {
	testPaths, err := filepath.Glob(filepath.FromSlash("testdata/check_files/*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, testPath := range testPaths {
		testPath := testPath
		name := strings.TrimSuffix(filepath.Base(testPath), ".txt")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Load the test.
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file %s", testPath)
			files := make([]fakeFile, 0, len(test.archive.Files))
			for _, tf := range test.archive.Files {
				files = append(files, fakeFile{
					name: tf.Name,
					size: uint64(len(tf.Data)),
					data: tf.Data,
				})
			}

			// Check the files.
			cf, err := modzip.CheckFiles[fakeFile](files, fakeFileIO{})
			got := formatCheckedFiles(cf)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("unexpected result; (-want +got):\n%s", diff)
			}
			// Check that the error (if any) is just a list of invalid files.
			// SizeError is not covered in this test.
			var gotErr string
			wantErr := test.wantErr
			if wantErr == "" && len(cf.Invalid) > 0 {
				wantErr = modzip.FileErrorList(cf.Invalid).Error()
			}
			if err := cf.Err(); err != nil {
				gotErr = err.Error()
			}
			if gotErr != wantErr {
				t.Errorf("got error:\n%s\n\nwant error:\n%s", gotErr, wantErr)
			}
		})
	}
}

// TestCheckDir verifies behavior of the CheckDir function. Note that CheckDir
// relies on CheckFiles and listFilesInDir (called by CreateFromDir), so this
// test focuses on how multiple errors and omissions are reported, rather than
// trying to cover every case.
func TestCheckDir(t *testing.T) {
	testPaths, err := filepath.Glob(filepath.FromSlash("testdata/check_dir/*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, testPath := range testPaths {
		testPath := testPath
		name := strings.TrimSuffix(filepath.Base(testPath), ".txt")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Load the test and extract the files to a temporary directory.
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file %s", testPath)
			tmpDir, err := extractTxtarToTempDir(t, test.archive)
			if err != nil {
				t.Fatal(err)
			}

			// Check the directory.
			cf, err := modzip.CheckDir(tmpDir)
			rep := strings.NewReplacer(tmpDir, "$work", `'\''`, `'\''`, string(os.PathSeparator), "/")
			got := rep.Replace(formatCheckedFiles(cf))
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("unexpected result; (-want +got):\n%s", diff)
			}

			// Check that the error (if any) is just a list of invalid files.
			// SizeError is not covered in this test.
			var gotErr string
			wantErr := test.wantErr
			if wantErr == "" && len(cf.Invalid) > 0 {
				wantErr = modzip.FileErrorList(cf.Invalid).Error()
			}
			if err := cf.Err(); err != nil {
				gotErr = err.Error()
			}
			if gotErr != wantErr {
				t.Errorf("got error:\n%s\n\nwant error:\n%s", gotErr, wantErr)
			}
		})
	}
}

// TestCheckZip verifies behavior of CheckZip. Note that CheckZip is also
// covered by TestUnzip, so this test focuses on how multiple errors are
// reported, rather than trying to cover every case.
func TestCheckZip(t *testing.T) {
	testPaths, err := filepath.Glob(filepath.FromSlash("testdata/check_zip/*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, testPath := range testPaths {
		testPath := testPath
		name := strings.TrimSuffix(filepath.Base(testPath), ".txt")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Load the test and extract the files to a temporary zip file.
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file %s", testPath)
			tmpZipPath, err := extractTxtarToTempZip(t, test.archive)
			if err != nil {
				t.Fatal(err)
			}

			// Check the zip.
			m := module.MustNewVersion(test.path, test.version)
			cf, checkZipErr := modzip.CheckZipFile(m, tmpZipPath)
			got := formatCheckedFiles(cf)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("unexpected result; (-want +got):\n%s", diff)
			}

			// Check that the error (if any) is just a list of invalid files.
			// SizeError is not covered in this test.
			var gotErr string
			wantErr := test.wantErr
			if wantErr == "" && len(cf.Invalid) > 0 {
				wantErr = modzip.FileErrorList(cf.Invalid).Error()
			}
			if checkZipErr != nil {
				gotErr = checkZipErr.Error()
			} else if err := cf.Err(); err != nil {
				gotErr = err.Error()
			}
			if gotErr != wantErr {
				t.Errorf("got error:\n%s\n\nwant error:\n%s", gotErr, wantErr)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	testDir := filepath.FromSlash("testdata/create")
	testInfos, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, testInfo := range testInfos {
		testInfo := testInfo
		base := filepath.Base(testInfo.Name())
		if filepath.Ext(base) != ".txt" {
			continue
		}
		t.Run(base[:len(base)-len(".txt")], func(t *testing.T) {
			t.Parallel()

			// Load the test.
			testPath := filepath.Join(testDir, testInfo.Name())
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file: %s", testPath)

			// Write zip to temporary file.
			tmpZipFile := tempFile(t, "TestCreate-*.zip")
			m := module.MustNewVersion(test.path, test.version)
			files := make([]fakeFile, len(test.archive.Files))
			for i, tf := range test.archive.Files {
				files[i] = fakeFile{
					name: tf.Name,
					size: uint64(len(tf.Data)),
					data: tf.Data,
				}
			}
			if err := modzip.Create[fakeFile](tmpZipFile, m, files, fakeFileIO{}); err != nil {
				if test.wantErr == "" {
					t.Fatalf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("got error %q; want error containing %q", err.Error(), test.wantErr)
				} else {
					return
				}
			} else if test.wantErr != "" {
				t.Fatalf("unexpected success; wanted error containing %q", test.wantErr)
			}
			if err := tmpZipFile.Close(); err != nil {
				t.Fatal(err)
			}

			// Hash zip file, compare with known value.
			if hash, err := dirhash.HashZip(tmpZipFile.Name(), dirhash.Hash1); err != nil {
				t.Fatal(err)
			} else if hash != test.hash {
				t.Errorf("got hash: %q\nwant: %q", hash, test.hash)
			}
			assertNoExcludedFiles(t, tmpZipFile.Name())
		})
	}
}

func assertNoExcludedFiles(t *testing.T, zf string) {
	z, err := zip.OpenReader(zf)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if shouldExclude(f) {
			t.Errorf("file %s should have been excluded but was not", f.Name)
		}
	}
}

func shouldExclude(f *zip.File) bool {
	r, err := f.Open()
	if err != nil {
		panic(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return bytes.Contains(data, []byte("excluded"))
}

func TestCreateFromDir(t *testing.T) {
	testDir := filepath.FromSlash("testdata/create_from_dir")
	testInfos, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, testInfo := range testInfos {
		testInfo := testInfo
		base := filepath.Base(testInfo.Name())
		if filepath.Ext(base) != ".txt" {
			continue
		}
		t.Run(base[:len(base)-len(".txt")], func(t *testing.T) {
			t.Parallel()

			// Load the test.
			testPath := filepath.Join(testDir, testInfo.Name())
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file %s", testPath)

			// Write files to a temporary directory.
			tmpDir, err := extractTxtarToTempDir(t, test.archive)
			if err != nil {
				t.Fatal(err)
			}

			// Create zip from the directory.
			tmpZipFile := tempFile(t, "TestCreateFromDir-*.zip")
			m := module.MustNewVersion(test.path, test.version)
			if err := modzip.CreateFromDir(tmpZipFile, m, tmpDir); err != nil {
				if test.wantErr == "" {
					t.Fatalf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("got error %q; want error containing %q", err, test.wantErr)
				} else {
					return
				}
			} else if test.wantErr != "" {
				t.Fatalf("unexpected success; want error containing %q", test.wantErr)
			}

			// Hash zip file, compare with known value.
			if hash, err := dirhash.HashZip(tmpZipFile.Name(), dirhash.Hash1); err != nil {
				t.Fatal(err)
			} else if hash != test.hash {
				t.Fatalf("got hash: %q\nwant: %q", hash, test.hash)
			}
			assertNoExcludedFiles(t, tmpZipFile.Name())
		})
	}
}

func TestCreateFromDirSpecial(t *testing.T) {
	for _, test := range []struct {
		desc     string
		setup    func(t *testing.T, tmpDir string) string
		wantHash string
	}{
		{
			desc: "ignore_empty_dir",
			setup: func(t *testing.T, tmpDir string) string {
				if err := os.Mkdir(filepath.Join(tmpDir, "empty"), 0777); err != nil {
					t.Fatal(err)
				}
				mustWriteFile(
					filepath.Join(tmpDir, "cue.mod/module.cue"),
					`module: "example.com/m"`,
				)
				return tmpDir
			},
			wantHash: "h1:vEUjl4tTsFcZJC/Ed/Rph2nVDCMG7OFC4wrQDfxF3n0=",
		}, {
			desc: "ignore_symlink",
			setup: func(t *testing.T, tmpDir string) string {
				if err := os.Symlink(tmpDir, filepath.Join(tmpDir, "link")); err != nil {
					switch runtime.GOOS {
					case "plan9", "windows":
						t.Skipf("could not create symlink: %v", err)
					default:
						t.Fatal(err)
					}
				}
				mustWriteFile(
					filepath.Join(tmpDir, "cue.mod/module.cue"),
					`module: "example.com/m"`,
				)
				return tmpDir
			},
			wantHash: "h1:vEUjl4tTsFcZJC/Ed/Rph2nVDCMG7OFC4wrQDfxF3n0=",
		}, {
			desc: "dir_is_vendor",
			setup: func(t *testing.T, tmpDir string) string {
				vendorDir := filepath.Join(tmpDir, "vendor")
				if err := os.Mkdir(vendorDir, 0777); err != nil {
					t.Fatal(err)
				}
				mustWriteFile(
					filepath.Join(vendorDir, "cue.mod/module.cue"),
					`module: "example.com/m"`,
				)
				return vendorDir
			},
			wantHash: "h1:vEUjl4tTsFcZJC/Ed/Rph2nVDCMG7OFC4wrQDfxF3n0=",
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			tmpDir := t.TempDir()
			dir := test.setup(t, tmpDir)

			tmpZipFile := tempFile(t, "TestCreateFromDir-*.zip")
			m := module.MustNewVersion("example.com/m@v1", "v1.0.0")

			if err := modzip.CreateFromDir(tmpZipFile, m, dir); err != nil {
				t.Fatal(err)
			}
			if err := tmpZipFile.Close(); err != nil {
				t.Fatal(err)
			}

			if hash, err := dirhash.HashZip(tmpZipFile.Name(), dirhash.Hash1); err != nil {
				t.Fatal(err)
			} else if hash != test.wantHash {
				t.Fatalf("got hash %q; want %q", hash, test.wantHash)
			}
		})
	}
}

func TestUnzip(t *testing.T) {
	testDir := filepath.FromSlash("testdata/unzip")
	testInfos, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, testInfo := range testInfos {
		base := filepath.Base(testInfo.Name())
		if filepath.Ext(base) != ".txt" {
			continue
		}
		t.Run(base[:len(base)-len(".txt")], func(t *testing.T) {
			// Load the test.
			testPath := filepath.Join(testDir, testInfo.Name())
			test, err := readTest(testPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("test file %s", testPath)

			// Convert txtar to temporary zip file.
			tmpZipPath, err := extractTxtarToTempZip(t, test.archive)
			if err != nil {
				t.Fatal(err)
			}

			// Extract to a temporary directory.
			tmpDir := t.TempDir()
			m := module.MustNewVersion(test.path, test.version)
			if err := modzip.Unzip(tmpDir, m, tmpZipPath); err != nil {
				if test.wantErr == "" {
					t.Fatalf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("got error %q; want error containing %q", err.Error(), test.wantErr)
				} else {
					return
				}
			} else if test.wantErr != "" {
				t.Fatalf("unexpected success; wanted error containing %q", test.wantErr)
			}

			// Hash the directory, compare to known value.
			if hash, err := dirhash.HashDir(tmpDir, "", dirhash.Hash1); err != nil {
				t.Fatal(err)
			} else if hash != test.hash {
				t.Fatalf("got hash %q\nwant: %q", hash, test.hash)
			}
		})
	}
}

type sizeLimitTest struct {
	desc              string
	files             []fakeFile
	wantErr           string
	wantCheckFilesErr string
	wantCreateErr     string
	wantCheckZipErr   string
	wantUnzipErr      string
}

// sizeLimitTests is shared by TestCreateSizeLimits and TestUnzipSizeLimits.
var sizeLimitTests = [...]sizeLimitTest{
	{
		desc: "one_large",
		files: []fakeFile{{
			name: "large.go",
			size: modzip.MaxZipFile - uint64(len(`module: "example.com/m@v1"`)),
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
	}, {
		desc: "total_large",
		files: []fakeFile{{
			name: "large.go",
			size: modzip.MaxZipFile - uint64(len(`module: "example.com/m@v1"`)),
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
	}, {
		desc: "total_too_large",
		files: []fakeFile{{
			name: "large.go",
			size: modzip.MaxZipFile - uint64(len(`module: "example.com/m@v1"`)) + 1,
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
		wantCheckFilesErr: "module source tree too large",
		wantCreateErr:     "module source tree too large",
		wantCheckZipErr:   "total uncompressed size of module contents too large",
		wantUnzipErr:      "total uncompressed size of module contents too large",
	}, {
		desc: "large_cuemod",
		files: []fakeFile{{
			name: "cue.mod/module.cue",
			size: modzip.MaxCUEMod,
		}},
	}, {
		desc: "too_large_cuemod",
		files: []fakeFile{{
			name: "cue.mod/module.cue",
			size: modzip.MaxCUEMod + 1,
		}},
		wantErr: "cue.mod/module.cue file too large",
	}, {
		desc: "large_license",
		files: []fakeFile{{
			name: "LICENSE",
			size: modzip.MaxLICENSE,
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
	}, {
		desc: "too_large_license",
		files: []fakeFile{{
			name: "LICENSE",
			size: modzip.MaxLICENSE + 1,
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
		wantErr: "LICENSE file too large",
	},
}

var sizeLimitVersion = module.MustNewVersion("example.com/large@v1", "v1.0.0")

func TestCreateSizeLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("creating large files takes time")
	}
	tests := append(sizeLimitTests[:], sizeLimitTest{
		// negative file size may happen when size is represented as uint64
		// but is cast to int64, as is the case in zip files.
		desc: "negative",
		files: []fakeFile{{
			name: "neg.go",
			size: 0x8000000000000000,
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
		wantErr: "module source tree too large",
	}, sizeLimitTest{
		desc: "size_is_a_lie",
		files: []fakeFile{{
			name: "lie.go",
			size: 1,
			data: []byte(`package large`),
		}, {
			name: "cue.mod/module.cue",
			data: []byte(`module: "example.com/m@v1"`),
		}},
		wantCreateErr: "larger than declared size",
	})

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			wantCheckFilesErr := test.wantCheckFilesErr
			if wantCheckFilesErr == "" {
				wantCheckFilesErr = test.wantErr
			}
			if _, err := modzip.CheckFiles[fakeFile](test.files, fakeFileIO{}); err == nil && wantCheckFilesErr != "" {
				t.Fatalf("CheckFiles: unexpected success; want error containing %q", wantCheckFilesErr)
			} else if err != nil && wantCheckFilesErr == "" {
				t.Fatalf("CheckFiles: got error %q; want success", err)
			} else if err != nil && !strings.Contains(err.Error(), wantCheckFilesErr) {
				t.Fatalf("CheckFiles: got error %q; want error containing %q", err, wantCheckFilesErr)
			}

			wantCreateErr := test.wantCreateErr
			if wantCreateErr == "" {
				wantCreateErr = test.wantErr
			}
			if err := modzip.Create[fakeFile](io.Discard, sizeLimitVersion, test.files, fakeFileIO{}); err == nil && wantCreateErr != "" {
				t.Fatalf("Create: unexpected success; want error containing %q", wantCreateErr)
			} else if err != nil && wantCreateErr == "" {
				t.Fatalf("Create: got error %q; want success", err)
			} else if err != nil && !strings.Contains(err.Error(), wantCreateErr) {
				t.Fatalf("Create: got error %q; want error containing %q", err, wantCreateErr)
			}
		})
	}
}

func TestUnzipSizeLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("creating large files takes time")
	}
	for _, test := range sizeLimitTests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			tmpZipFile := tempFile(t, "TestUnzipSizeLimits-*.zip")

			zw := zip.NewWriter(tmpZipFile)
			for _, tf := range test.files {
				zf, err := zw.Create(fakeFileIO{}.Path(tf))
				if err != nil {
					t.Fatal(err)
				}
				rc, err := fakeFileIO{}.Open(tf)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(zf, rc)
				rc.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := tmpZipFile.Close(); err != nil {
				t.Fatal(err)
			}

			tmpDir := t.TempDir()

			wantCheckZipErr := test.wantCheckZipErr
			if wantCheckZipErr == "" {
				wantCheckZipErr = test.wantErr
			}
			cf, err := modzip.CheckZipFile(sizeLimitVersion, tmpZipFile.Name())
			if err == nil {
				err = cf.Err()
			}
			if err == nil && wantCheckZipErr != "" {
				t.Fatalf("CheckZip: unexpected success; want error containing %q", wantCheckZipErr)
			} else if err != nil && wantCheckZipErr == "" {
				t.Fatalf("CheckZip: got error %q; want success", err)
			} else if err != nil && !strings.Contains(err.Error(), wantCheckZipErr) {
				t.Fatalf("CheckZip: got error %q; want error containing %q", err, wantCheckZipErr)
			}

			wantUnzipErr := test.wantUnzipErr
			if wantUnzipErr == "" {
				wantUnzipErr = test.wantErr
			}
			if err := modzip.Unzip(tmpDir, sizeLimitVersion, tmpZipFile.Name()); err == nil && wantUnzipErr != "" {
				t.Fatalf("Unzip: unexpected success; want error containing %q", wantUnzipErr)
			} else if err != nil && wantUnzipErr == "" {
				t.Fatalf("Unzip: got error %q; want success", err)
			} else if err != nil && !strings.Contains(err.Error(), wantUnzipErr) {
				t.Fatalf("Unzip: got error %q; want error containing %q", err, wantUnzipErr)
			}
		})
	}
}

func TestUnzipSizeLimitsSpecial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test; creating large files takes time")
	}

	for _, test := range []struct {
		desc     string
		wantErr  string
		m        module.Version
		writeZip func(t *testing.T, zipFile *os.File)
	}{
		{
			desc: "large_zip",
			m:    module.MustNewVersion("example.com/m@v1", "v1.0.0"),
			writeZip: func(t *testing.T, zipFile *os.File) {
				if err := zipFile.Truncate(modzip.MaxZipFile); err != nil {
					t.Fatal(err)
				}
			},
			// this is not an error we care about; we're just testing whether
			// Unzip checks the size of the file before opening.
			// It's harder to create a valid zip file of exactly the right size.
			wantErr: "not a valid zip file",
		}, {
			desc: "too_large_zip",
			m:    module.MustNewVersion("example.com/m@v1", "v1.0.0"),
			writeZip: func(t *testing.T, zipFile *os.File) {
				if err := zipFile.Truncate(modzip.MaxZipFile + 1); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "module zip file is too large",
		}, {
			desc: "size_is_a_lie",
			m:    module.MustNewVersion("example.com/m@v1", "v1.0.0"),
			writeZip: func(t *testing.T, zipFile *os.File) {
				// Create a normal zip file in memory containing one file full of zero
				// bytes. Use a distinctive size so we can find it later.
				zipBuf := &bytes.Buffer{}
				zw := zip.NewWriter(zipBuf)
				f, err := zw.Create("cue.mod/module.cue")
				if err != nil {
					t.Fatal(err)
				}
				realSize := 0x0BAD
				buf := make([]byte, realSize)
				if _, err := f.Write(buf); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}

				// Replace the uncompressed size of the file. As a shortcut, we just
				// search-and-replace the byte sequence. It should occur twice because
				// the 32- and 64-byte sizes are stored separately. All multi-byte
				// values are little-endian.
				zipData := zipBuf.Bytes()
				realSizeData := []byte{0xAD, 0x0B}
				fakeSizeData := []byte{0xAC, 0x00}
				s := zipData
				n := 0
				for {
					if i := bytes.Index(s, realSizeData); i < 0 {
						break
					} else {
						s = s[i:]
					}
					copy(s[:len(fakeSizeData)], fakeSizeData)
					n++
				}
				if n != 2 {
					t.Fatalf("replaced size %d times; expected 2", n)
				}

				// Write the modified zip to the actual file.
				if _, err := zipFile.Write(zipData); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "not a valid zip file",
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			tmpZipFile := tempFile(t, "TestUnzipSizeLimits-*.zip")
			test.writeZip(t, tmpZipFile)
			if err := tmpZipFile.Close(); err != nil {
				t.Fatal(err)
			}

			tmpDir := t.TempDir()

			if err := modzip.Unzip(tmpDir, test.m, tmpZipFile.Name()); err == nil && test.wantErr != "" {
				t.Fatalf("unexpected success; want error containing %q", test.wantErr)
			} else if err != nil && test.wantErr == "" {
				t.Fatalf("got error %q; want success", err)
			} else if err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("got error %q; want error containing %q", err, test.wantErr)
			}
		})
	}
}

func mustWriteFile(name string, content string) {
	if err := os.MkdirAll(filepath.Dir(name), 0o777); err != nil {
		panic(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o666); err != nil {
		panic(err)
	}
}

func tempFile(t *testing.T, tmpl string) *os.File {
	f, err := os.CreateTemp("", tmpl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.Close()
		os.Remove(f.Name())
	})
	return f
}
