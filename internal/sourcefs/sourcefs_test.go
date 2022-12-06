package sourcefs

import (
	"io"
	"io/fs"
	"log"
	"path"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
)

func TestSourceFS(t *testing.T) {
	m := map[string]Source{
		"foo/bar/baz":   FromString("baz contents"),
		"foo/bar/dir/p": FromString("p contents"),
		"a":             FromString("a contents"),
	}
	sf, err := newSourceFS(m)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	fs.WalkDir(sf, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatalf("error walking to %q: %v", path, err)
		}
		dp := "- "
		if d.IsDir() {
			dp = "d "
		}
		paths = append(paths, dp+path)
		return nil
	})
	want := []string{
		"d .",
		"- a",
		"d foo",
		"d foo/bar",
		"- foo/bar/baz",
		"d foo/bar/dir",
		"- foo/bar/dir/p",
	}
	if diff := cmp.Diff(want, paths); diff != "" {
		t.Errorf("unexpected dir contents; (-want, +got):\n%s", diff)
	}

	for _, p := range want {
		isDir := p[0] == 'd'
		p = p[2:]
		info, err := fs.Stat(sf, p)
		if err != nil {
			log.Fatal(err)
		}
		if got, want := info.IsDir(), isDir; got != want {
			t.Fatalf("unexpected isDir for %q; got %v want %v", p, got, want)
		}
		if !isDir {
			checkFileContent(t, sf, p, path.Base(p)+" contents")
		}
	}
}

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
