package load

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/mod/module"
	"golang.org/x/mod/zip"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/runtime"
)

// registryClient implements the protocol for talking to
// the registry server.
type registryClient struct {
	// TODO caching
	registryURL string
	cacheDir    string
}

// newRegistryClient returns a registry client that talks to
// the given base URL and stores downloaded module information
// in the given cache directory. It assumes that information
// in the registry is immutable, so if it's in the cache, a module
// will not be downloaded again.
func newRegistryClient(registryURL, cacheDir string) *registryClient {
	return &registryClient{
		registryURL: registryURL,
		cacheDir:    cacheDir,
	}
}

// fetchModFile returns the parsed contents of the cue.mod/module.cue file
// for the given module.
func (c *registryClient) fetchModFile(m module.Version) (*modFile, error) {
	data, err := c.fetchRawModFile(m)
	if err != nil {
		return nil, err
	}
	mf, err := parseModuleFile(data, path.Join(m.Path, "cue.mod/module.cue"))
	if err != nil {
		return nil, err
	}
	return mf, nil
}

// fetchModFile returns the contents of the cue.mod/module.cue file
// for the given module without parsing it.
func (c *registryClient) fetchRawModFile(m module.Version) ([]byte, error) {
	resp, err := http.Get(c.registryURL + "/" + m.Path + "/@v/" + m.Version + ".mod")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot get HTTP response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("module.cue HTTP GET request failed: %s", body)
	}
	return body, nil
}

// getModContents downloads the module with the given version
// and returns the directory where it's stored.
func (c *registryClient) getModContents(m module.Version) (string, error) {
	modPath := filepath.Join(c.cacheDir, fmt.Sprintf("%s@%s", m.Path, m.Version))
	if _, err := os.Stat(modPath); err == nil {
		return modPath, nil
	}
	// TODO synchronize parallel invocations
	resp, err := http.Get(c.registryURL + "/" + m.Path + "/@v/" + m.Version + ".zip")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("module.cue HTTP GET request failed: %s", body)
	}
	zipfile := filepath.Join(c.cacheDir, m.String()+".zip")
	if err := os.MkdirAll(filepath.Dir(zipfile), 0o777); err != nil {
		return "", fmt.Errorf("cannot create parent directory for zip file: %v", err)
	}
	f, err := os.Create(zipfile)
	if err != nil {
		return "", fmt.Errorf("cannot create zipfile: %v", err)
	}

	defer f.Close() // TODO check error on close
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("cannot copy data to zip file %q: %v", zipfile, err)
	}
	if err := zip.Unzip(modPath, m, zipfile); err != nil {
		return "", fmt.Errorf("cannot unzip %v: %v", m, err)
	}
	return modPath, nil
}

// parseModuleFile parses a cue.mod/module.cue file.
// TODO move this to be closer to the modFile type definition.
func parseModuleFile(data []byte, filename string) (*modFile, error) {
	file, err := parser.ParseFile(filename, data)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file %q", data)
	}
	// TODO disallow non-data-mode CUE.

	ctx := (*cue.Context)(runtime.New())
	schemav := ctx.CompileBytes(moduleSchema, cue.Filename("$cueroot/cue/load/moduleschema.cue"))
	if err := schemav.Validate(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "internal error: invalid CUE module.cue schema")
	}
	v := ctx.BuildFile(file)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file")
	}
	v = v.Unify(schemav)
	if err := v.Validate(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file")
	}
	var mf modFile
	if err := v.Decode(&mf); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct (\nfile %q\ncontents %q\nvalue %#v\n)", filename, data, v)
	}
	return &mf, nil
}
