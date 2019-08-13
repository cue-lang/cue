// Copyright 2019 The CUE Authors
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

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		// TODO: this command is still experimental, don't show it in
		// the documentation just yet.
		Hidden: true,

		Use:   "add <glob> [--list]",
		Short: "bulk append to CUE files",
		Long: `Append a common snippet of CUE to many files and commit atomically.
`,
		RunE: runAdd,
	}

	f := cmd.Flags()
	f.Bool(string(flagList), false,
		"text executed as Go template with instance info")
	f.BoolP(string(flagDryrun), "n", false,
		"only run simulation")

	return cmd
}

func runAdd(cmd *cobra.Command, args []string) (err error) {
	return doAdd(cmd, stdin, args)
}

var stdin io.Reader = os.Stdin

func doAdd(cmd *cobra.Command, stdin io.Reader, args []string) (err error) {
	// Offsets at which to restore original files, if any, if any of the
	// appends fail.
	// Ideally this is placed below where it is used, but we want to make
	// absolutely sure that the error variable used in defer is the named
	// returned value and not some shadowed value.

	originals := []originalFile{}
	defer func() {
		if err != nil {
			restoreOriginals(cmd, originals)
		}
	}()

	// build instance cache
	builds := map[string]*build.Instance{}

	getBuild := func(path string) *build.Instance {
		if b, ok := builds[path]; ok {
			return b
		}
		b := load.Instances([]string{path}, nil)[0]
		builds[path] = b
		return b
	}

	// determine file set.

	todo := []*fileInfo{}

	done := map[string]bool{}

	for _, arg := range args {
		dir, base := filepath.Split(arg)
		dir = filepath.Clean(dir)
		matches, err := filepath.Glob(dir)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if fi, err := os.Stat(m); err != nil || !fi.IsDir() {
				continue
			}
			file := filepath.Join(m, base)
			if done[file] {
				continue
			}
			if s := filepath.ToSlash(file); strings.HasPrefix(s, "pkg/") || strings.Contains(s, "/pkg/") {
				continue
			}
			done[file] = true
			fi, err := initFile(cmd, file, getBuild)
			if err != nil {
				return err
			}
			todo = append(todo, fi)
			b := fi.build
			if flagList.Bool(cmd) && (b == nil) {
				return fmt.Errorf("instance info not available for %s", fi.filename)
			}
		}
	}

	// Read text to be appended.
	text, err := ioutil.ReadAll(stdin)
	if err != nil {
		return err
	}

	var tmpl *template.Template
	if flagList.Bool(cmd) {
		tmpl, err = template.New("append").Parse(string(text))
		if err != nil {
			return err
		}
	}

	for _, fi := range todo {
		if tmpl == nil {
			fi.contents.Write(text)
			continue
		}
		if err := tmpl.Execute(fi.contents, fi.build); err != nil {
			return err
		}
	}

	if flagDryrun.Bool(cmd) {
		stdout := cmd.OutOrStdout()
		for _, fi := range todo {
			fmt.Fprintln(stdout, "---", fi.filename)
			io.Copy(stdout, fi.contents)
		}
		return nil
	}

	// All verified. Execute the todo plan
	for _, fi := range todo {
		fo, err := fi.appendAndCheck()
		if err != nil {
			return err
		}
		originals = append(originals, fo)
	}

	// Verify resulting builds
	for _, fi := range todo {
		builds = map[string]*build.Instance{}

		b := getBuild(fi.buildArg)
		if b.Err != nil {
			return b.Err
		}
		i := cue.Build([]*build.Instance{b})[0]
		if i.Err != nil {
			return i.Err
		}
		if err := i.Value().Validate(); err != nil {
			return i.Err
		}
	}

	return nil
}

type originalFile struct {
	filename string
	contents []byte
}

func restoreOriginals(cmd *cobra.Command, originals []originalFile) {
	for _, fo := range originals {
		if err := fo.restore(); err != nil {
			fmt.Fprintln(cmd.OutOrStderr(), "Error restoring file: ", err)
		}
	}
}

func (fo *originalFile) restore() error {
	if len(fo.contents) == 0 {
		return os.Remove(fo.filename)
	}
	return ioutil.WriteFile(fo.filename, fo.contents, 0644)
}

type fileInfo struct {
	filename string
	buildArg string
	contents *bytes.Buffer
	build    *build.Instance
}

func initFile(cmd *cobra.Command, file string, getBuild func(path string) *build.Instance) (todo *fileInfo, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("init file: %v", err)
		}
	}()
	dir := filepath.Dir(file)
	todo = &fileInfo{file, dir, &bytes.Buffer{}, nil}

	if fi, err := os.Stat(file); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// File does not exist
		b := getBuild(dir)
		todo.build = b
		pkg := flagPackage.String(cmd)
		if pkg != "" {
			// TODO: do something more intelligent once the package name is
			// computed on a module basis, even for empty directories.
			b.PkgName = pkg
			b.Err = nil
		} else {
			pkg = b.PkgName
		}
		if pkg == "" {
			return nil, errors.New("must specify package using -p for new files")
		}
		todo.buildArg = file
		fmt.Fprintf(todo.contents, "package %s\n\n", pkg)
	} else {
		if fi.IsDir() {
			return nil, fmt.Errorf("cannot append to directory %s", file)
		}

		f, err := parser.ParseFile(file, nil)
		if err != nil {
			return nil, err
		}
		if _, pkgName, _ := internal.PackageInfo(f); pkgName != "" {
			if pkg := flagPackage.String(cmd); pkg != "" && pkgName != pkg {
				return nil, fmt.Errorf("package mismatch (%s vs %s) for file %s", pkgName, pkg, file)
			}
			todo.build = getBuild(dir)
		} else {
			if pkg := flagPackage.String(cmd); pkg != "" {
				return nil, fmt.Errorf("file %s has no package clause but package %s requested", file, pkg)
			}
			todo.build = getBuild(file)
			todo.buildArg = file
		}
	}
	return todo, nil
}

func (fi *fileInfo) appendAndCheck() (fo originalFile, err error) {
	// Read original file
	b, err := ioutil.ReadFile(fi.filename)
	if err == nil {
		fo.filename = fi.filename
		fo.contents = b
	} else if !os.IsNotExist(err) {
		return originalFile{}, err
	}

	if !bytes.HasSuffix(b, []byte("\n")) {
		b = append(b, '\n')
	}
	b = append(b, fi.contents.Bytes()...)

	b, err = format.Source(b)
	if err != nil {
		return originalFile{}, err
	}

	if err = ioutil.WriteFile(fi.filename, b, 0644); err != nil {
		// Just in case, attempt to restore original file.
		fo.restore()
		return originalFile{}, err
	}

	return fo, nil
}
