// Command testscript-plugin-cue is a testscript plugin (see
// github.com/rogpeppe/go-internal/testscript/plugin) that mirrors the registry-related
// testscript support used by the cmd/cue tests in cuelang.org/go.
//
// It does not provide a cue command: a test runs its own cue executable via
// testscript's "exec cue". The plugin's job is to provision the environment
// that cue needs, in particular:
//
//   - per-test OCI module registries derived from _registry* directories,
//     exposed via CUE_REGISTRY* environment variables;
//   - the CUE_CACHE_DIR, CUE_LANGUAGE_VERSION and CUE_LANGUAGE_VERSION_BUGFIX
//     environment variables;
//   - a "memregistry" command that starts an in-memory OCI registry on demand
//     and sets a CUE_REGISTRY* variable to its host.
//
// Use it from a testscript test by enabling the plugin command (see
// github.com/rogpeppe/go-internal/testscript/plugin) and starting a
// script with "plugin cue".
package main

import (
	"fmt"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"cuelang.org/go/mod/modregistrytest"
	"github.com/rogpeppe/go-internal/testscript/plugin"
)

func main() {
	if err := plugin.Serve(cuePlugin{}); err != nil {
		fmt.Fprintln(os.Stderr, "testscript-plugin-cue:", err)
		os.Exit(1)
	}
}

// maxRegistries bounds the number of numbered registry environment variables
// (CUE_REGISTRY, CUE_REGISTRY1, ...) that the plugin declares it may set. It
// corresponds to the number of _registry* directories a single test is
// expected to use.
const maxRegistries = 10

// resultEnv holds the environment variables that the plugin is allowed to set
// in the test environment. It is an explicit allowlist rather than a wildcard
// so that the set of variables the plugin can affect is clear.
var resultEnv = buildResultEnv()

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

// inProcessRunners holds the commands provided by the plugin, keyed by name.
var inProcessRunners = map[string]func(inst *cueInstance, p plugin.CmdParams) (plugin.CmdResult, error){
	"memregistry": cmdMemRegistry,
}

type cuePlugin struct{}

func (cuePlugin) Info() plugin.PluginInfo {
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

func (cuePlugin) NewTestInstance(p plugin.TestParams) (plugin.TestInstance, error) {
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

func (cuePlugin) Close() {}

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

// startRegistry starts a fake OCI module registry serving the modules found in
// fsys (optionally under a path prefix), registering its cleanup with the
// instance and returning its host. If useProxy is set, the registry is fronted
// by an OCI proxy server, mirroring the way the Central Registry works.
func (inst *cueInstance) startRegistry(fsys fs.FS, prefix string, useProxy bool) (string, error) {
	reg, err := modregistrytest.New(fsys, prefix)
	if err != nil {
		return "", fmt.Errorf("cannot start test registry server: %v", err)
	}
	inst.addCloser(reg.Close)
	host := reg.Host()
	if useProxy {
		proxyClient, err := ociclient.New(host, &ociclient.Options{Insecure: true})
		if err != nil {
			return "", fmt.Errorf("cannot create oci proxy client: %v", err)
		}
		proxy := httptest.NewServer(ociserver.New(proxyClient, nil))
		u, _ := url.Parse(proxy.URL)
		host = u.Host
		inst.addCloser(proxy.Close)
	}
	return host, nil
}

// cmdMemRegistry implements the memregistry command: it starts an
// in-memory OCI registry and sets the named environment variable to its
// host. The variable name must be one the plugin declares it may set
// (the CUE_REGISTRY* family); see resultEnv.
func cmdMemRegistry(inst *cueInstance, p plugin.CmdParams) (plugin.CmdResult, error) {
	usage := func() (plugin.CmdResult, error) {
		return plugin.CmdResult{}, fmt.Errorf("usage: memregistry [-auth=username:password] <envvar-name>")
	}
	args := p.Args
	var auth *modregistrytest.AuthConfig
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		userPass, ok := strings.CutPrefix(args[0], "-auth=")
		if !ok {
			return usage()
		}
		user, pass, ok := strings.Cut(userPass, ":")
		if !ok {
			return usage()
		}
		auth = &modregistrytest.AuthConfig{Username: user, Password: pass}
		args = args[1:]
	}
	if len(args) != 1 {
		return usage()
	}
	srv, err := modregistrytest.NewServer(ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: true}), auth)
	if err != nil {
		return plugin.CmdResult{}, fmt.Errorf("cannot start registrytest server: %v", err)
	}
	inst.addCloser(srv.Close)
	return plugin.CmdResult{
		Env: map[string]string{
			args[0]: srv.Host(),
		},
	}, nil
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
