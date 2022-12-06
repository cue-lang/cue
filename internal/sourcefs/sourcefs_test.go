package sourcefs

import (
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/internal/ospath"
)

func TestOSSourceFSUnix(t *testing.T) {
	newFS := func(m map[string]Source) (fs.FS, error) {
		return newOSSourceFS(m, ospath.Unix, func(p string) (string, error) {
			if ospath.Unix.IsAbs(p) {
				return p, nil
			}
			return ospath.Unix.Join([]string{"/root/dir", p}), nil
		})
	}
	testSourceFS(t, newFS, []string{
		"foo/bar/baz",
		"foo/bar/dir/p",
		"a",
		"/blah",
	}, []string{
		"d .",
		"- blah /blah",
		"d root",
		"d root/dir",
		"- root/dir/a a",
		"d root/dir/foo",
		"d root/dir/foo/bar",
		"- root/dir/foo/bar/baz foo/bar/baz",
		"d root/dir/foo/bar/dir",
		"- root/dir/foo/bar/dir/p foo/bar/dir/p",
	})
}

func TestOSSourceFSWindows(t *testing.T) {
	newFS := func(m map[string]Source) (fs.FS, error) {
		return newOSSourceFS(m, ospath.Windows, func(p string) (string, error) {
			if ospath.Windows.IsAbs(p) {
				return p, nil
			}
			return ospath.Unix.Join([]string{"c:\\", p}), nil
		})
	}
	testSourceFS(t, newFS, []string{
		`foo\bar\baz`,
		`foo\bar\dir\p`,
		`d:\something`,
		`//network/share/x/y`,
		`\\network\share\x\z`,
		`a`,
		`\blah`,
	}, []string{
		`d .`,
		`d \\network\share`,
		`d \\network\share/x`,
		`- \\network\share/x/y //network/share/x/y`,
		`- \\network\share/x/z \\network\share\x\z`,
		`d c:`,
		`- c:/a a`,
		`- c:/blah \blah`,
		`d c:/foo`,
		`d c:/foo/bar`,
		`- c:/foo/bar/baz foo\bar\baz`,
		`d c:/foo/bar/dir`,
		`- c:/foo/bar/dir/p foo\bar\dir\p`,
		`d d:`,
		`- d:/something d:\something`,
	})
}

func TestSourceFS(t *testing.T) {
	testSourceFS(t, NewSourceFS, []string{
		"foo/bar/baz",
		"foo/bar/dir/p",
		"a",
	}, []string{
		"d .",
		"- a a",
		"d foo",
		"d foo/bar",
		"- foo/bar/baz foo/bar/baz",
		"d foo/bar/dir",
		"- foo/bar/dir/p foo/bar/dir/p",
	})
}

func TestSourceFSClash(t *testing.T) {
	_, err := NewSourceFS(map[string]Source{
		"x":   FromString("x contents"),
		"x/y": FromString("y contents"),
	})
	assertErrorMatches(t, err, `file "x" has another file nested within it`)
}

func TestOSSourceFSDuplicateEntry(t *testing.T) {
	_, err := newOSSourceFS(map[string]Source{
		"/x/y":  FromString("x contents"),
		"/x//y": FromString("y contents"),
	}, ospath.Unix, nopAbs)
	// Note: we might get either error because the map is traversed in arbitrary order.
	assertErrorMatches(t, err, `duplicate file overlay entry for "/x/y" \(clashes with "/x//y"\)|duplicate file overlay entry for "/x//y" \(clashes with "/x/y"\)`)
}

func TestSourceFSInvalidPath(t *testing.T) {
	_, err := NewSourceFS(map[string]Source{
		"/x": FromString("x contents"),
	})
	assertErrorMatches(t, err, `"/x" is not a valid io/fs.FS path`)
}

func TestSourceFSPathNotFound(t *testing.T) {
	sf, err := NewSourceFS(map[string]Source{
		"x/y": FromString("x contents"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sf.Open("x/z")
	assertErrorMatches(t, err, `open x/z: file does not exist`)

	_, err = sf.Open("x/y/z")
	assertErrorMatches(t, err, `open x/y/z: file does not exist`)
}

func testSourceFS(t *testing.T, newFS func(m map[string]Source) (fs.FS, error), paths, wantEntries []string) {
	m := make(map[string]Source)
	for _, p := range paths {
		elem := p
		if i := strings.LastIndexAny(p, `/\`); i >= 0 {
			elem = p[i+1:]
		}
		m[p] = FromString(elem + " contents")
	}
	sf, err := newFS(m)
	if err != nil {
		t.Fatal(err)
	}
	var gotEntries []string
	fs.WalkDir(sf, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatalf("error walking to %q: %v", path, err)
		}
		dp := "- "
		if d.IsDir() {
			dp = "d "
		}
		dp += path
		f, err := sf.Open(path)
		if err != nil {
			t.Fatalf("cannot open path %q", path)
		}
		defer f.Close()
		if f, ok := f.(SourceFile); ok {
			origPath := f.OriginalPath()
			if origPath != "" {
				dp += " " + origPath
			}
		}
		gotEntries = append(gotEntries, dp)
		return nil
	})
	if diff := cmp.Diff(wantEntries, gotEntries); diff != "" {
		t.Errorf("unexpected dir contents; (-want, +got):\n%s", diff)
	}

	for _, p := range wantEntries {
		fields := strings.Fields(p)
		isDir := fields[0] == "d"
		p := fields[1]
		info, err := fs.Stat(sf, p)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.IsDir(), isDir; got != want {
			t.Fatalf("unexpected isDir for %q; got %v want %v", p, got, want)
		}
		if !isDir {
			checkFileContent(t, sf, p, path.Base(p)+" contents")
		}
	}
}

// checkFileContent checks that the path p within sf has the given content
// and original path.
func checkFileContent(t *testing.T, sf fs.FS, p string, wantContent string) {
	data, err := fs.ReadFile(sf, p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), wantContent; got != want {
		t.Fatalf("unexpected content for %q; got %q want %q", p, got, want)
	}
	// Check we can read it in small amounts.
	f, err := sf.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err = io.ReadAll(iotest.OneByteReader(f))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), wantContent; got != want {
		t.Fatalf("unexpected content for %q; got %q want %q", p, got, want)
	}

	// Check we can seek back to the start.
	n, err := f.(io.ReadSeeker).Seek(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := n, int64(0); got != want {
		t.Fatalf("unexpected content for %q; got %q want %q", p, got, want)
	}
	data, err = io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), wantContent; got != want {
		t.Fatalf("unexpected content for %q; got %q want %q", p, got, want)
	}
}

func assertErrorMatches(t *testing.T, err error, pat string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, not nil")
	}
	re, err1 := regexp.Compile("^(" + pat + ")$")
	if err1 != nil {
		t.Fatal(err)
	}
	if !re.MatchString(err.Error()) {
		t.Fatalf("error %q does not match %q", err.Error(), pat)
	}
}

func nopAbs(p string) (string, error) {
	return path.Clean(p), nil
}
