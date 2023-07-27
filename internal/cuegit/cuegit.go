// cuegit reverts test files in the git working tree that only differ by
// the performance stats. This is useful during development to filter out
// noise.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"golang.org/x/tools/txtar"
)

var (
	verbose = flag.Bool("v", false, "verbose output")
)

func try[T any](x T, e error) T {
	if e != nil {
		panic(e)
	}
	return x
}

func println(args ...interface{}) {
	if *verbose {
		fmt.Println(args...)
	}
}

func main() {
	flag.Parse()

	rep := try(git.PlainOpen("."))
	base := staged(rep)

	for f, s := range try(try(rep.Worktree()).Status()) {
		if path.Ext(f) != ".txtar" {
			continue
		}

		if s.Worktree != git.Modified {
			continue
		}

		diffFile(base, rep, f)
	}
}

type hasher func(string) plumbing.Hash

func staged(rep *git.Repository) hasher {
	index := map[string]plumbing.Hash{}
	for _, e := range try(rep.Storer.Index()).Entries {
		index[e.Name] = e.Hash
	}
	return func(path string) plumbing.Hash {
		return index[path]
	}
}

func worktree(rep *git.Repository) hasher {
	commit := try(rep.CommitObject(try(rep.Head()).Hash()))

	return func(path string) plumbing.Hash {
		return try(commit.File(path)).Hash
	}
}

func diffFile(base hasher, rep *git.Repository, filepath string) {
	println("===", filepath)

	obj := try(rep.Storer.EncodedObject(plumbing.BlobObject, base(filepath)))
	r := try(obj.Reader())
	defer r.Close()

	b := try(ioutil.ReadAll(r))

	aa := txtar.Parse(b)

	ab := try(txtar.ParseFile(filepath))

	m := make(map[string]*txtar.File)
	for _, f := range aa.Files {
		f := f
		m[f.Name] = &f
	}

	for _, f := range ab.Files {
		if strings.HasSuffix(f.Name, "/stats") {
			continue
		}
		g, ok := m[f.Name]
		if !ok {
			println("   ", "missing", f.Name)
			return
		}

		if diff := bytes.Compare(f.Data, g.Data); diff != 0 {
			println("   ", f.Name, "changed")
			return
		}
	}

	// Only stats changed, revert the file.
	os.WriteFile(filepath, b, 0644)
	println("    reverted", filepath)
}
