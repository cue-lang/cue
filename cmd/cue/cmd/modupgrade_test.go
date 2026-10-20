// Copyright 2026 The CUE Authors
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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/tools/fix"
)

// TestModUpgradePolicy observes what cue mod upgrade writes, reports and
// returns in the cases a script cannot reach, because no file that parses
// fails to rewrite today: the rewrite is driven through the rewriteInstances
// seam, which the fixture directs.
//
// Each fixture under testdata/modupgrade is a txtar archive whose comment
// holds directives, one per line:
//
//	fail <path>      the rewrite of this file fails
//	invalid <path>   the rewrite of this file produces output the target rejects
//	readonly <path>  this file cannot be written (chmod 0444)
//	force            pass --force
//	f                pass -f, the short spelling of --force
//	exit <n>         the expected exit status of the first run (default 0)
//	rerun            run again with the seam and the read-only bits removed,
//	                 expecting exit status 0 and the files under expect2/
//
// The directives end at the first blank line; the rest of the comment
// describes the fixture.
//
// After a run, every file of the archive outside expect/ and expect2/ must
// equal expect/<path> (expect2/<path> after the rerun) when that section
// exists, and its original content otherwise. Standard error must equal
// expect/stderr (expect2/stderr).
func TestModUpgradePolicy(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/modupgrade/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Not(qt.HasLen(fixtures, 0)))
	for _, path := range fixtures {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".txtar"), func(t *testing.T) {
			runPolicyFixture(t, path)
		})
	}
}

func runPolicyFixture(t *testing.T, path string) {
	ar, err := txtar.ParseFile(path)
	qt.Assert(t, qt.IsNil(err))

	var failPath, invalidPath, readonlyPath, forceFlag string
	rerun := false
	wantExit := 0
	// The directives are the comment's first paragraph; what follows the
	// first blank line describes the fixture.
	for line := range strings.SplitSeq(string(ar.Comment), "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		arg := ""
		if len(fields) > 1 {
			arg = fields[1]
		}
		switch fields[0] {
		case "fail":
			failPath = arg
		case "invalid":
			invalidPath = arg
		case "readonly":
			readonlyPath = arg
		case "force":
			forceFlag = "--force"
		case "f":
			forceFlag = "-f"
		case "rerun":
			rerun = true
		case "exit":
			wantExit, err = strconv.Atoi(arg)
			qt.Assert(t, qt.IsNil(err))
		default:
			t.Fatalf("unknown directive %q", line)
		}
	}
	if readonlyPath != "" && (runtime.GOOS == "windows" || os.Geteuid() == 0) {
		t.Skip("read-only files are not refused to this user")
	}

	// The command resolves symbolic links in the module root, and on some
	// systems the temporary directory is reached through one.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	qt.Assert(t, qt.IsNil(err))
	orig := map[string][]byte{}
	for _, f := range ar.Files {
		if strings.HasPrefix(f.Name, "expect/") || strings.HasPrefix(f.Name, "expect2/") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(f.Name))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, f.Data, 0o666)))
		orig[f.Name] = f.Data
	}
	section := func(name string) ([]byte, bool) {
		for _, f := range ar.Files {
			if f.Name == name {
				return f.Data, true
			}
		}
		return nil, false
	}
	check := func(prefix string, wantExit int, gotExit int, stderr string) {
		t.Helper()
		qt.Check(t, qt.Equals(gotExit, wantExit))
		wantErr, _ := section(prefix + "/stderr")
		qt.Check(t, qt.Equals(stderr, string(wantErr)))
		for name, data := range orig {
			want := data
			if d, ok := section(prefix + "/" + name); ok {
				want = d
			}
			got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
			qt.Assert(t, qt.IsNil(err))
			qt.Check(t, qt.Equals(string(got), string(want)), qt.Commentf("file %s", name))
		}
	}

	// The seam: fail or spoil the named file after the real rewrite ran.
	real := rewriteInstances
	seam := func(insts []*build.Instance, o ...fix.Option) errors.Error {
		errs := real(insts, o...)
		for _, b := range insts {
			for _, f := range b.Files {
				rel, _ := filepath.Rel(dir, f.Filename)
				switch filepath.ToSlash(rel) {
				case failPath:
					errs = errors.Append(errs, errors.Newf(f.Pos(), "injected rewrite failure"))
				case invalidPath:
					// A prefix alias is what v0.18.0 stopped accepting.
					bad, err := parser.ParseFile("bad.cue", "package p\n\nX=a: 1\n", parser.Version("v0.17.0"))
					if err != nil {
						panic(err)
					}
					f.Decls = bad.Decls
				}
			}
		}
		return errs
	}
	run := func(withSeam bool) (int, string) {
		t.Helper()
		if withSeam {
			rewriteInstances = seam
		} else {
			rewriteInstances = real
		}
		args := []string{"mod", "upgrade"}
		if forceFlag != "" {
			args = append(args, forceFlag)
		}
		args = append(args, "v0.18.0")
		c, err := New(args)
		qt.Assert(t, qt.IsNil(err))
		var stdout, stderr bytes.Buffer
		c.root.SetErr(&stderr)
		c.SetOut(&stdout)
		err = c.Run(t.Context())
		if err != nil && err != ErrPrintedError {
			errors.Print(&stderr, err, &errors.Config{Cwd: dir, ToSlash: true})
		}
		// The command speaks on stderr only.
		qt.Check(t, qt.Equals(stdout.String(), ""))
		exit := 0
		if err != nil {
			exit = 1
		}
		return exit, stderr.String()
	}
	t.Cleanup(func() { rewriteInstances = real })
	t.Chdir(dir)

	if readonlyPath != "" {
		p := filepath.Join(dir, filepath.FromSlash(readonlyPath))
		qt.Assert(t, qt.IsNil(os.Chmod(p, 0o444)))
	}
	exit, stderr := run(true)
	check("expect", wantExit, exit, stderr)

	if !rerun {
		return
	}
	if readonlyPath != "" {
		p := filepath.Join(dir, filepath.FromSlash(readonlyPath))
		qt.Assert(t, qt.IsNil(os.Chmod(p, 0o666)))
	}
	exit, stderr = run(false)
	check("expect2", 0, exit, stderr)
}
