// Command testscript-plugin-cue is a [testscript plugin] that provides
// a CUE registry within a testscript. It offers the same capabilities used
// in the CUE command tests.
//
// It does not provide a cue command: a test runs its own cue executable via
// testscript's "exec cue". The plugin's job is to provision the environment
// that cue needs, in particular:
//
//   - per-test OCI module registries derived from _registry* directories,
//     exposed via CUE_REGISTRY* environment variables;
//   - the CUE_CACHE_DIR, CUE_LANGUAGE_VERSION and CUE_LANGUAGE_VERSION_BUGFIX
//     environment variables;
//   - the commands provided by [cuetestscript.Cmds], such as "memregistry",
//     which starts an in-memory OCI registry on demand and sets a
//     CUE_REGISTRY* variable to its host.
//
// This is usable out-of-the-box with the [testscript command], or can be used in
// other situations by enabling the [testscript plugin] and starting a
// script with "plugin cue".
//
// [testscript plugin]: https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript/plugin
// [testscript command]: https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/testscript
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/rogpeppe/go-internal/testscript/plugin"

	"cuelang.org/go/internal/cuetestscript"
)

func main() {
	if err := plugin.Serve(cuePlugin{}); err != nil {
		fmt.Fprintln(os.Stderr, "testscript-plugin-cue:", err)
		os.Exit(1)
	}
}

// cuePlugin implements [plugin.Interface] to provide the cue plugin
// functionality to testscript.
type cuePlugin struct{}

func (cuePlugin) Info() plugin.PluginInfo {
	cmds := make(map[string]plugin.CmdInfo)
	for name, cmd := range cuetestscript.Cmds() {
		cmds[name] = plugin.CmdInfo{
			RequiredEnv:  set(cmd.RequiredEnv),
			WritesOutput: cmd.WritesOutput,
		}
	}
	return plugin.PluginInfo{
		RequiredEnv: map[string]bool{"WORK": true},
		// The plugin sets only what the shared environment sets, so
		// the allowlist follows from it rather than being maintained
		// here.
		ResultEnv: set(cuetestscript.ResultEnv()),
		Cmds:      cmds,
	}
}

func (cuePlugin) NewTestInstance(p plugin.TestParams) (plugin.TestInstance, error) {
	work := p.Env["WORK"]
	if work == "" {
		return nil, fmt.Errorf("the cue plugin requires the WORK environment variable")
	}
	inst := &cueInstance{
		work: work,
		env:  make(map[string]string),
	}
	if err := cuetestscript.Setup(inst); err != nil {
		inst.Close()
		return nil, err
	}
	return inst, nil
}

func (cuePlugin) Close() {}

// cueInstance holds the state of the plugin for a single test. It implements
// both [plugin.TestInstance] and [cuetestscript.Env], accumulating the
// environment that the shared setup sets so that it can be handed to the host.
type cueInstance struct {
	work string

	// mu guards the fields below it.
	mu sync.Mutex
	// env holds the environment variables set during setup.
	env map[string]string
	// closers holds cleanup functions for services started either during
	// setup or by a command.
	closers []func()
}

func (inst *cueInstance) WorkDir() string {
	return inst.work
}

func (inst *cueInstance) Getenv(key string) string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.env[key]
}

func (inst *cueInstance) Setenv(key, value string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.env[key] = value
}

func (inst *cueInstance) Defer(f func()) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closers = append(inst.closers, f)
}

func (inst *cueInstance) Env() map[string]string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return maps.Clone(inst.env)
}

func (inst *cueInstance) Close() {
	inst.mu.Lock()
	closers := inst.closers
	inst.closers = nil
	inst.mu.Unlock()
	// Close in reverse order of registration.
	for i := len(closers) - 1; i >= 0; i-- {
		closers[i]()
	}
}

func (inst *cueInstance) RunCmd(p plugin.CmdParams) (plugin.CmdResult, error) {
	cmd, ok := cuetestscript.Cmds()[p.Name]
	if !ok {
		return plugin.CmdResult{}, fmt.Errorf("unrecognized command %q", p.Name)
	}
	e := &cmdEnv{
		cueInstance: inst,
		dir:         p.Dir,
		env:         p.Env,
	}
	err := cmd.Run(e, p.Args)
	if errors.Is(err, cuetestscript.ErrUsage) {
		// Unlike a command failure, incorrect usage is not something a
		// script can expect with "!", so report it as a plugin error.
		return plugin.CmdResult{}, fmt.Errorf("usage: %s", cmd.Usage)
	}
	res := plugin.CmdResult{
		Stdout: e.stdout.Bytes(),
		Stderr: e.stderr.Bytes(),
		Env:    e.setEnv,
	}
	if err != nil {
		res.Err = err.Error()
	}
	return res, nil
}

// cmdEnv implements [cuetestscript.CmdEnv] for a single command run. Unlike
// setup, a command's environment changes and output are reported back to the
// host in its result, so they are accumulated here rather than in the instance.
type cmdEnv struct {
	*cueInstance
	dir            string
	env            map[string]string
	setEnv         map[string]string
	stdout, stderr bytes.Buffer
}

func (e *cmdEnv) Getenv(key string) string {
	return e.env[key]
}

func (e *cmdEnv) Setenv(key, value string) {
	if e.setEnv == nil {
		e.setEnv = make(map[string]string)
	}
	e.setEnv[key] = value
}

func (e *cmdEnv) MkAbs(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(e.dir, path)
}

func (e *cmdEnv) Stdout() io.Writer {
	return &e.stdout
}

func (e *cmdEnv) Stderr() io.Writer {
	return &e.stderr
}

func set(names []string) map[string]bool {
	m := make(map[string]bool)
	for _, name := range names {
		m[name] = true
	}
	return m
}
