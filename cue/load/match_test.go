// Copyright 2018 The CUE Authors
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

package load

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	c := &Config{}
	what := "default"
	matchFn := func(tag string, want map[string]bool) {
		t.Helper()
		m := make(map[string]bool)
		if !doMatch(c, tag, m) {
			t.Errorf("%s context should match %s, does not", what, tag)
		}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("%s tags = %v, want %v", tag, m, want)
		}
	}
	noMatch := func(tag string, want map[string]bool) {
		m := make(map[string]bool)
		if doMatch(c, tag, m) {
			t.Errorf("%s context should NOT match %s, does", what, tag)
		}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("%s tags = %v, want %v", tag, m, want)
		}
	}

	c.BuildTags = []string{"foo"}
	matchFn("foo", map[string]bool{"foo": true})
	noMatch("!foo", map[string]bool{"foo": true})
	matchFn("foo,!bar", map[string]bool{"foo": true, "bar": true})
	noMatch("!", map[string]bool{})
}

func TestShouldBuild(t *testing.T) {
	const file1 = "// +build tag1\n\n" +
		"package main\n"
	want1 := map[string]bool{"tag1": true}

	const file2 = "// +build cgo\n\n" +
		"// This package implements parsing of tags like\n" +
		"// +build tag1\n" +
		"package load"
	want2 := map[string]bool{"cgo": true}

	const file3 = "// Copyright The CUE Authors.\n\n" +
		"package load\n\n" +
		"// shouldBuild checks tags given by lines of the form\n" +
		"// +build tag\n" +
		"func shouldBuild(content []byte)\n"
	want3 := map[string]bool{}

	c := &Config{BuildTags: []string{"tag1"}}
	m := map[string]bool{}
	if !shouldBuild(c, []byte(file1), m) {
		t.Errorf("shouldBuild(file1) = false, want true")
	}
	if !reflect.DeepEqual(m, want1) {
		t.Errorf("shouldBuild(file1) tags = %v, want %v", m, want1)
	}

	m = map[string]bool{}
	if shouldBuild(c, []byte(file2), m) {
		t.Errorf("shouldBuild(file2) = true, want false")
	}
	if !reflect.DeepEqual(m, want2) {
		t.Errorf("shouldBuild(file2) tags = %v, want %v", m, want2)
	}

	m = map[string]bool{}
	c = &Config{BuildTags: nil}
	if !shouldBuild(c, []byte(file3), m) {
		t.Errorf("shouldBuild(file3) = false, want true")
	}
	if !reflect.DeepEqual(m, want3) {
		t.Errorf("shouldBuild(file3) tags = %v, want %v", m, want3)
	}
}

type readNopCloser struct {
	io.Reader
}

func (r readNopCloser) Close() error {
	return nil
}

var (
	cfg    = &Config{BuildTags: []string{"enable"}}
	defCfg = &Config{}
)

var matchFileTests = []struct {
	cfg   *Config
	name  string
	data  string
	match bool
}{
	{defCfg, "foo.cue", "", true},
	{defCfg, "a/b/c/foo.cue", "// +build enable\n\npackage foo\n", false},
	{defCfg, "foo.cue", "// +build !enable\n\npackage foo\n", true},
	{defCfg, "foo1.cue", "// +build linux\n\npackage foo\n", false},
	{defCfg, "foo.badsuffix", "", false},
	{cfg, "a/b/c/d/foo.cue", "// +build enable\n\npackage foo\n", true},
	{cfg, "foo.cue", "// +build !enable\n\npackage foo\n", false},
}

func TestMatchFile(t *testing.T) {
	cwd, _ := os.Getwd()
	abs := func(path string) string {
		return filepath.Join(cwd, path)
	}
	for _, tt := range matchFileTests {
		cfg := tt.cfg
		cfg.Overlay = map[string]Source{abs(tt.name): FromString(tt.data)}
		cfg, _ = cfg.complete()

		match, err := matchFileTest(cfg, "", tt.name)
		if match != tt.match || err != nil {
			t.Fatalf("MatchFile(%q) = %v, %v, want %v, nil", tt.name, match, err, tt.match)
		}
	}
}
