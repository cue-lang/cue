package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript/plugin"
)

// maxRegistries bounds the number of numbered registry environment variables
// (CUE_REGISTRY, CUE_REGISTRY1, ...) that the plugin declares it may set. It
// corresponds to the number of _registry* directories a single test is
// expected to use.
const maxRegistries = 10

// resultEnv holds the environment variables that the plugin is allowed to set
// in the test environment. It is an explicit allowlist rather than a wildcard
// so that the set of variables the plugin can affect is clear.
var resultEnv = buildResultEnv()

// inProcessRunners holds the commands provided by the plugin, keyed by name.
var inProcessRunners = map[string]func(inst *cueInstance, p plugin.CmdParams) (plugin.CmdResult, error){
	"memregistry": cmdMemRegistry,
}

func buildResultEnv() map[string]bool {
	m := map[string]bool{
		"CUE_CACHE_DIR": true,
	}
	// CUE_REGISTRY* and DEBUG_REGISTRY*_HOST for each registry id.
	for _, id := range registryIDs() {
		m["CUE_REGISTRY"+id] = true
		m["DEBUG_REGISTRY"+id+"_HOST"] = true
	}
	return m
}

// registryIDs returns the registry ids used to form CUE_REGISTRY* variable
// names: the empty id (CUE_REGISTRY) plus "0" to "maxRegistries-1".
func registryIDs() []string {
	ids := []string{""}
	for i := range maxRegistries {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}

type cuePlugin struct{}

func newPlugin() *cuePlugin { return &cuePlugin{} }

func (*cuePlugin) Info() plugin.PluginInfo {
	cmds := make(map[string]plugin.CmdInfo)
	for name := range inProcessRunners {
		cmds[name] = plugin.CmdInfo{}
	}
	return plugin.PluginInfo{
		RequiredEnv: map[string]bool{"WORK": true},
		ResultEnv:   resultEnv,
		Cmds:        cmds,
	}
}

func (*cuePlugin) NewTestInstance(p plugin.TestParams) (plugin.TestInstance, error) {
	work := p.Env["WORK"]
	if work == "" {
		return nil, fmt.Errorf("the cue plugin requires the WORK environment variable")
	}
	inst := &cueInstance{
		work: work,
		env:  make(map[string]string),
	}

	// If a testscript loads CUE packages but forgot to set up a cue.mod, we
	// might walk up to the system's temporary directory looking for cue.mod.
	// Make the parent of the work directory a module so behaviour is
	// consistent no matter what parent directories contain. Creating the
	// directory is enough; ignore ErrExist since only the first test wins.
	workdirRoot := filepath.Dir(work)
	if err := os.Mkdir(filepath.Join(workdirRoot, "cue.mod"), 0o777); err != nil && !os.IsExist(err) {
		inst.Close()
		return nil, err
	}

	// os.UserCacheDir relies on OS-specific env vars that we don't set, so
	// point the cache somewhere predictable within the work directory.
	inst.env["CUE_CACHE_DIR"] = filepath.Join(work, ".tmp/cache")

	if err := inst.setupRegistries(); err != nil {
		inst.Close()
		return nil, err
	}
	return inst, nil
}

func (*cuePlugin) Close() {}

type cueInstance struct {
	work string
	env  map[string]string

	// mu guards closers, which holds cleanup functions for services started
	// either during setup (registries) or by the memregistry command.
	mu      sync.Mutex
	closers []func()
}

func (inst *cueInstance) Env() map[string]string {
	return inst.env
}

func (inst *cueInstance) addCloser(f func()) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closers = append(inst.closers, f)
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
	run := inProcessRunners[p.Name]
	if run == nil {
		return plugin.CmdResult{}, fmt.Errorf("unrecognized command %q", p.Name)
	}
	return run(inst, p)
}

// setupRegistries scans the work directory for _registry* directories and
// starts a fake OCI registry server for each, mirroring the behaviour of the
// cmd/cue tests. The resulting CUE_REGISTRY* and DEBUG_REGISTRY*_HOST
// variables are added to the instance environment.
func (inst *cueInstance) setupRegistries() error {
	entries, err := os.ReadDir(inst.work)
	if err != nil {
		return fmt.Errorf("cannot read work directory: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		regID, ok := cutRegistryPrefix(entry.Name())
		if !ok {
			continue
		}
		registryDir := filepath.Join(inst.work, entry.Name())
		prefix := ""
		if data, err := os.ReadFile(filepath.Join(inst.work, "_registry"+regID+"_prefix")); err == nil {
			prefix = strings.TrimSpace(string(data))
		}
		useProxy := false
		proxyFile := filepath.Join(inst.work, "_registry"+regID+"_proxy")
		if data, err := os.ReadFile(proxyFile); err == nil {
			useProxy, err = strconv.ParseBool(strings.TrimSpace(string(data)))
			if err != nil {
				return fmt.Errorf("invalid contents of proxy file %q: %v", proxyFile, err)
			}
		}
		regHost, err := inst.startRegistry(os.DirFS(registryDir), prefix, useProxy)
		if err != nil {
			return err
		}
		regPrefix := ""
		if prefix != "" {
			regPrefix = "/" + prefix
		}
		inst.env["CUE_REGISTRY"+regID] = regHost + regPrefix + "+insecure"
		// This lets tests construct their own malformed CUE_REGISTRY values
		// that still refer to the test registry.
		inst.env["DEBUG_REGISTRY"+regID+"_HOST"] = regHost
	}
	return nil
}

// cutRegistryPrefix reports whether name is a "_registry<id>" directory and,
// if so, returns the id (which may be empty).
func cutRegistryPrefix(name string) (id string, ok bool) {
	const prefix = "_registry"
	if len(name) < len(prefix) || name[:len(prefix)] != prefix {
		return "", false
	}
	return name[len(prefix):], true
}
