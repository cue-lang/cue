// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	cuecmd "cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/lsp/cache"
)

var (
	runSubprocessTests       = flag.Bool("enable_cuelsp_subprocess_tests", false, "run integration tests against a cue lsp subprocess (default: in-process)")
	cueBinaryPath            = flag.String("cue_test_binary", "", "path to the cue binary for use as a remote, for use with the -enable_cuelsp_subprocess_tests flag")
	timeout                  = flag.Duration("timeout", defaultTimeout(), "if nonzero, default timeout for each integration test; defaults to CUELSP_INTEGRATION_TEST_TIMEOUT")
	skipCleanup              = flag.Bool("skip_cleanup", false, "whether to skip cleaning up temp directories")
	printGoroutinesOnFailure = flag.Bool("print_goroutines", false, "whether to print goroutines info on failure")
	printLogs                = flag.Bool("print_logs", false, "whether to print LSP logs")
)

func defaultTimeout() time.Duration {
	s := os.Getenv("CUELSP_INTEGRATION_TEST_TIMEOUT")
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid CUELSP_INTEGRATION_TEST_TIMEOUT %q: %v\n", s, err)
		os.Exit(2)
	}
	return d
}

var runner *Runner

// The integrationTestRunner interface abstracts the Run operation,
// enables decorators for various optional features.
type integrationTestRunner interface {
	Run(t *testing.T, files string, f TestFunc)
}

func Run(t *testing.T, files string, f TestFunc) {
	runner.Run(t, files, f)
}

func WithOptions(opts ...RunOption) configuredRunner {
	return configuredRunner{opts: opts}
}

type configuredRunner struct {
	opts []RunOption
}

func (r configuredRunner) Run(t *testing.T, files string, f TestFunc) {
	// Print a warning if the test's temporary directory is not
	// suitable as a workspace folder, as this may lead to
	// otherwise-cryptic failures. This situation typically occurs
	// when an arbitrary string (e.g. "foo.") is used as a subtest
	// name, on a platform with filename restrictions (e.g. no
	// trailing period on Windows).
	tmp := t.TempDir()
	if err := cache.CheckPathValid(tmp); err != nil {
		t.Logf("Warning: testing.T.TempDir(%s) is not valid as a workspace folder: %s",
			tmp, err)
	}

	runner.Run(t, files, f, r.opts...)
}

type RunMultiple []struct {
	Name   string
	Runner integrationTestRunner
}

func (r RunMultiple) Run(t *testing.T, files string, f TestFunc) {
	for _, runner := range r {
		t.Run(runner.Name, func(t *testing.T) {
			runner.Runner.Run(t, files, f)
		})
	}
}

// DefaultModes returns the default modes to run for each regression test (they
// may be reconfigured by the tests themselves).
func DefaultModes() Mode {
	modes := Default
	if !testing.Short() {
		modes |= Experimental | Forwarded
	}
	if *runSubprocessTests {
		modes |= SeparateProcess
	}
	return modes
}

// Main sets up and tears down the shared integration test state.
func Main(m *testing.M, hook func(*settings.Options)) {
	// If this magic environment variable is set, run cue lsp instead of the test
	// suite. See the documentation for runTestAsCueLspEnvvar for more details.
	if os.Getenv(runTestAsCueLspEnvvar) == "true" {
		// Run the real cue CLI: the arguments are of the form
		// "lsp serve ..." (see Runner.separateProcessServer). Note
		// that the hook parameter is not honored in this mode; it is
		// a no-op for all current callers.
		c, err := cuecmd.New(os.Args[1:])
		if err == nil {
			err = c.Run(context.Background())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	flag.Parse()

	runner = &Runner{
		DefaultModes:             DefaultModes(),
		Timeout:                  *timeout,
		PrintGoroutinesOnFailure: *printGoroutinesOnFailure,
		SkipCleanup:              *skipCleanup,
		OptionsHook:              hook,
	}

	runner.cuePath = *cueBinaryPath
	if runner.cuePath == "" {
		var err error
		runner.cuePath, err = os.Executable()
		if err != nil {
			panic(fmt.Sprintf("finding test binary path: %v", err))
		}
	}

	dir, err := os.MkdirTemp("", "cue-lsp-test-")
	if err != nil {
		panic(fmt.Errorf("creating temp directory: %v", err))
	}
	runner.tempDir = dir

	var code int
	defer func() {
		if err := runner.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "closing test runner: %v\n", err)
			// Cleanup sometimes flakes on Windows due to file locking, but this is OK for our CI.
			if runtime.GOOS != "windows" {
				os.Exit(1)
			}
		}
		os.Exit(code)
	}()
	code = m.Run()
}
