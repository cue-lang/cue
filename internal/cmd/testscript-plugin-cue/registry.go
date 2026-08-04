package main

import (
	"fmt"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"cuelang.org/go/mod/modregistrytest"
	"github.com/rogpeppe/go-internal/testscript/plugin"
)

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

// memregistry starts an in-memory OCI registry and sets the named environment
// variable to its host. The variable name must be one the plugin declares it
// may set (the CUE_REGISTRY* family); see resultEnv.
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
