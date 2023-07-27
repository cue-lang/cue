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

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func println(args ...interface{}) {
	if *verbose {
		fmt.Println(args...)
	}
}

func main() {
	flag.Parse()

	rep, err := git.PlainOpen(".")
	check(err)

	w, err := rep.Worktree()
	check(err)

	stat, err := w.Status()
	check(err)

	base := staged(rep)

	for f, s := range stat {
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
	idx, err := rep.Storer.Index()
	check(err)
	index := map[string]plumbing.Hash{}
	for _, e := range idx.Entries {
		index[e.Name] = e.Hash
	}
	return func(path string) plumbing.Hash {
		return index[path]
	}
}

func worktree(rep *git.Repository) hasher {
	ref, err := rep.Head()
	check(err)

	commit, err := rep.CommitObject(ref.Hash())
	check(err)

	return func(path string) plumbing.Hash {
		file, err := commit.File(path)
		check(err)
		return file.Hash
	}
}

func diffFile(base hasher, rep *git.Repository, filepath string) {
	println("===", filepath)

	hash := base(filepath)
	obj, err := rep.Storer.EncodedObject(plumbing.BlobObject, hash)
	check(err)
	r, err := obj.Reader()
	check(err)
	defer r.Close()

	b, err := ioutil.ReadAll(r)
	check(err)

	aa := txtar.Parse(b)
	check(err)

	ab, err := txtar.ParseFile(filepath)
	check(err)

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
