// Copyright 2019 CUE Authors
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

package cuetest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"github.com/kylelemons/godebug/diff"
)

type Config struct {
	Stdin  io.Reader
	Stdout io.Writer
	Golden string
}

// Run executes the given command in the given directory and reports any
// errors comparing it to the gold standard.
func Run(t *testing.T, dir, command string, cfg *Config) {
	if cfg == nil {
		cfg = &Config{}
	}

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { os.Chdir(old) }()

	logf(t, "Executing command: %s", command)

	command = strings.TrimSpace(command[4:])
	args := SplitArgs(t, command)
	logf(t, "Args: %q", args)

	buf := &bytes.Buffer{}
	if cfg.Golden != "" {
		if cfg.Stdout != nil {
			t.Fatal("cannot set Golden and Stdout")
		}
		cfg.Stdout = buf
	}
	cmd, err := cmd.New(args)
	if cfg.Stdout != nil {
		cmd.SetOutput(cfg.Stdout)
	}
	if cfg.Stdin != nil {
		cmd.SetInput(cfg.Stdin)
	}
	if err = cmd.Run(context.Background()); err != nil {
		logf(t, "Execution failed: %v", err)
	}

	if cfg.Golden == "" {
		return
	}

	pattern := fmt.Sprintf("//.*%s.*", regexp.QuoteMeta(dir))
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatal(err)
	}
	got := re.ReplaceAllString(buf.String(), "")
	got = strings.TrimSpace(got)

	want := strings.TrimSpace(cfg.Golden)
	if got != want {
		t.Errorf("files differ:\n%s", diff.Diff(want, got))
	}
}

func logf(t *testing.T, format string, args ...interface{}) {
	t.Logf(format, args...)
}

func SplitArgs(t *testing.T, s string) (args []string) {
	c := NewChunker(t, []byte(s))
	for {
		found := c.Find(" '")
		args = append(args, strings.Split(c.Text(), " ")...)
		if !found {
			break
		}
		c.Next("", "' ")
		args = append(args, c.Text())
	}
	return args
}
