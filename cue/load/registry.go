package load

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/modzip"
)

// registryClient implements the protocol for talking to
// the registry server.
type registryClient struct {
	// TODO caching
	client   *modregistry.Client
	cacheDir string
}

// newRegistryClient returns a registry client that talks to
// the given base URL and stores downloaded module information
// in the given cache directory. It assumes that information
// in the registry is immutable, so if it's in the cache, a module
// will not be downloaded again.
func newRegistryClient(registry ociregistry.Interface, cacheDir string) (*registryClient, error) {
	return &registryClient{
		client:   modregistry.NewClient(registry),
		cacheDir: cacheDir,
	}, nil
}

// fetchModFile returns the parsed contents of the cue.mod/module.cue file
// for the given module.
func (c *registryClient) fetchModFile(ctx context.Context, m module.Version) (*modfile.File, error) {
	data, err := c.fetchRawModFile(ctx, m)
	if err != nil {
		return nil, err
	}
	mf, err := modfile.Parse(data, path.Join(m.Path(), "cue.mod/module.cue"))
	if err != nil {
		return nil, err
	}
	return mf, nil
}

// fetchModFile returns the contents of the cue.mod/module.cue file
// for the given module without parsing it.
func (c *registryClient) fetchRawModFile(ctx context.Context, mv module.Version) ([]byte, error) {
	m, err := c.client.GetModule(ctx, mv)
	if err != nil {
		return nil, err
	}
	return m.ModuleFile(ctx)
}

// getModContents downloads the module with the given version
// and returns the directory where it's stored.
func (c *registryClient) getModContents(ctx context.Context, mv module.Version) (string, error) {
	modPath := filepath.Join(c.cacheDir, mv.String())
	if _, err := os.Stat(modPath); err == nil {
		return modPath, nil
	}
	m, err := c.client.GetModule(ctx, mv)
	if err != nil {
		return "", err
	}
	r, err := m.GetZip(ctx)
	if err != nil {
		return "", err
	}
	defer r.Close()
	zipfile := filepath.Join(c.cacheDir, mv.String()+".zip")
	if err := os.MkdirAll(filepath.Dir(zipfile), 0o777); err != nil {
		return "", fmt.Errorf("cannot create parent directory for zip file: %v", err)
	}
	f, err := os.Create(zipfile)
	if err != nil {
		return "", fmt.Errorf("cannot create zipfile: %v", err)
	}

	defer f.Close() // TODO check error on close
	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("cannot copy data to zip file %q: %v", zipfile, err)
	}
	if err := modzip.Unzip(modPath, mv, zipfile); err != nil {
		return "", fmt.Errorf("cannot unzip %v: %v", mv, err)
	}
	return modPath, nil
}
