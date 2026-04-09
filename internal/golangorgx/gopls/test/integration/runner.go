// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/lsprpc"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2/servertest"
	"cuelang.org/go/internal/lsp/cache"
	"github.com/go-quicktest/qt"
)

// Mode is a bitmask that defines for which execution modes a test should run.
type Mode int

const (
	// Default mode runs the server with the default options, communicating over
	// pipes to emulate the lsp sidecar execution mode, which communicates over
	// stdin/stdout.
	//
	// It uses separate servers for each test, but a shared cache, to avoid
	// duplicating work when processing GOROOT.
	Default Mode = 1 << iota

	// Forwarded is kept as a placeholder so that existing test code using
	// DefaultModes()&^Forwarded continues to compile. It is never enabled.
	Forwarded

	// Experimental enables all of the experimental configurations that are
	// being developed, and runs the server in sidecar mode.
	//
	// It uses a separate cache for each test, to exercise races that may only
	// appear with cache misses.
	Experimental
)

func (m Mode) String() string {
	switch m {
	case Default:
		return "default"
	case Experimental:
		return "experimental"
	default:
		return "unknown mode"
	}
}

// A Runner runs tests in server execution environments, as specified by its
// modes.
type Runner struct {
	// Configuration
	DefaultModes             Mode                    // modes to run for each test
	Timeout                  time.Duration           // per-test timeout, if set
	PrintGoroutinesOnFailure bool                    // whether to dump goroutines on test failure
	SkipCleanup              bool                    // if set, don't delete test data directories when the test exits
	OptionsHook              func(*settings.Options) // if set, use these options when creating gopls sessions

	tempDir string // shared parent temp directory
}

type TestFunc func(t *testing.T, env *Env)

// Run executes the test function in the default configured gopls execution
// modes. For each a test run, a new workspace is created containing the
// un-txtared files specified by filedata.
func (r *Runner) Run(t *testing.T, files string, test TestFunc, opts ...RunOption) {
	// TODO(rfindley): this function has gotten overly complicated, and warrants
	// refactoring.
	t.Helper()

	tests := []struct {
		name      string
		mode      Mode
		getServer func(runConfig, func(*settings.Options)) jsonrpc2.StreamServer
	}{
		{"default", Default, r.defaultServer},
		{"experimental", Experimental, r.experimentalServer},
	}

	for _, tc := range tests {
		config := defaultConfig()
		for _, opt := range opts {
			opt.set(&config)
		}
		modes := r.DefaultModes
		if config.modes != 0 {
			modes = config.modes
		}
		if modes&tc.mode == 0 {
			continue
		}

		t.Run(tc.name, func(t *testing.T) {
			// TODO(rfindley): once jsonrpc2 shutdown is fixed, we should not leak
			// goroutines in this test function.
			// stacktest.NoLeak(t)

			ctx := context.Background()
			if r.Timeout != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, r.Timeout)
				defer cancel()
			} else if d, ok := t.Deadline(); ok {
				timeout := time.Until(d) * 19 / 20 // Leave an arbitrary 5% for cleanup.
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			rootDir := filepath.Join(r.tempDir, filepath.FromSlash(t.Name()))
			if err := os.MkdirAll(rootDir, 0755); err != nil {
				t.Fatal(err)
			}

			files := fake.UnpackTxt(files)
			if config.editor.WindowsLineEndings {
				for name, data := range files {
					files[name] = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
				}
			}
			config.sandbox.Files = files
			config.sandbox.RootDir = rootDir
			sandbox, err := fake.NewSandbox(&config.sandbox)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if !r.SkipCleanup {
					if err := sandbox.Close(); err != nil {
						pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
						t.Errorf("closing the sandbox: %v", err)
					}
				}
			}()

			ss := tc.getServer(config, r.OptionsHook)

			framer := jsonrpc2.NewRawStream
			ls := &loggingFramer{}
			framer = ls.framer(jsonrpc2.NewRawStream)
			ts := servertest.NewPipeServer(ss, framer)

			awaiter := NewAwaiter(sandbox.Workdir)
			const skipApplyEdits = false
			editor, err := fake.NewEditor(sandbox, config.editor).Connect(ctx, ts, awaiter.Hooks(), skipApplyEdits)

			// Were we expecting an error?
			if config.initializeErrorMatches != "" {
				qt.Assert(t, qt.ErrorMatches(err, config.initializeErrorMatches))
				// at this point we are done
				return

			} else {
				qt.Assert(t, qt.IsNil(err))
			}

			env := &Env{
				T:       t,
				Ctx:     ctx,
				Sandbox: sandbox,
				Editor:  editor,
				Server:  ts,
				Awaiter: awaiter,
			}
			defer func() {
				if t.Failed() && r.PrintGoroutinesOnFailure {
					pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
				}
				if t.Failed() || *printLogs {
					ls.printBuffers(t.Name(), os.Stderr)
				}
				// For tests that failed due to a timeout, don't fail to shutdown
				// because ctx is done.
				//
				// There is little point to setting an arbitrary timeout for closing
				// the editor: in general we want to clean up before proceeding to the
				// next test, and if there is a deadlock preventing closing it will
				// eventually be handled by the `go test` timeout.
				if err := editor.Close(context.WithoutCancel(ctx)); err != nil {
					t.Errorf("closing editor: %v", err)
				}
			}()
			// Always await the initial workspace load.
			env.Await(InitialWorkspaceLoad)
			test(t, env)
		})
	}
}

type loggingFramer struct {
	mu  sync.Mutex
	buf *safeBuffer
}

// safeBuffer is a threadsafe buffer for logs.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (s *loggingFramer) framer(f jsonrpc2.Framer) jsonrpc2.Framer {
	return func(nc net.Conn) jsonrpc2.Stream {
		s.mu.Lock()
		framed := false
		if s.buf == nil {
			s.buf = &safeBuffer{buf: bytes.Buffer{}}
			framed = true
		}
		s.mu.Unlock()
		stream := f(nc)
		if framed {
			return protocol.LoggingStream(stream, s.buf)
		}
		return stream
	}
}

func (s *loggingFramer) printBuffers(testname string, w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buf == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "#### Start Gopls Test Logs for %q\n", testname)
	s.buf.mu.Lock()
	io.Copy(w, &s.buf.buf)
	s.buf.mu.Unlock()
	fmt.Fprintf(os.Stderr, "#### End Gopls Test Logs for %q\n", testname)
}

// defaultServer handles the Default execution mode.
func (r *Runner) defaultServer(config runConfig, optsHook func(*settings.Options)) jsonrpc2.StreamServer {
	c, err := newCache(config)
	if err != nil {
		panic(err)
	}
	return lsprpc.NewStreamServer(c, false, optsHook)
}

// experimentalServer handles the Experimental execution mode.
func (r *Runner) experimentalServer(config runConfig, optsHook func(*settings.Options)) jsonrpc2.StreamServer {
	c, err := newCache(config)
	if err != nil {
		panic(err)
	}
	options := func(o *settings.Options) {
		optsHook(o)
		o.EnableAllExperiments()
	}
	return lsprpc.NewStreamServer(c, false, options)
}

func newCache(config runConfig) (*cache.Cache, error) {
	if config.reg == nil {
		return cache.New(nil)
	} else {
		return cache.NewWithRegistry(nil, config.reg), nil
	}
}

// Close cleans up resources that have been allocated to this workspace.
func (r *Runner) Close() error {
	if !r.SkipCleanup {
		if err := os.RemoveAll(r.tempDir); err != nil {
			return fmt.Errorf("errors closing the test runner:\n\t%s", err)
		}
	}
	return nil
}
