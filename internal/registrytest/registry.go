package registrytest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/zip"
)

// New starts a registry instance that serves modules found inside the
// _registry path inside fsys. It serves the OCI registry protocol.
//
// Each module should be inside a directory named path_vers, where
// slashes in path have been replaced with underscores and should
// contain a cue.mod/module.cue file holding the module info.
//
// The Registry should be closed after use.
func New(fsys fs.FS) (*Registry, error) {
	r := ocimem.New()
	client, err := modregistry.NewClient(r)
	if err != nil {
		return nil, fmt.Errorf("cannot make client: %v", err)
	}
	mods, err := getModules(fsys)
	if err != nil {
		return nil, fmt.Errorf("invalid modules: %v", err)
	}
	if err := pushContent(client, mods); err != nil {
		return nil, fmt.Errorf("cannot push modules: %v", err)
	}
	srv := httptest.NewServer(ociserver.New(r, nil))
	u, err := url.Parse(srv.URL)
	if err != nil {
		return nil, err
	}
	return &Registry{
		srv:  srv,
		host: u.Host,
	}, nil
}

func pushContent(client *modregistry.Client, mods map[module.Version]*moduleContent) error {
	pushed := make(map[module.Version]bool)
	for v := range mods {
		err := visitDepthFirst(mods, v, func(v module.Version, m *moduleContent) error {
			if pushed[v] {
				return nil
			}
			var zipContent bytes.Buffer
			if err := m.writeZip(&zipContent); err != nil {
				return err
			}
			if err := client.PutModule(context.Background(), v, bytes.NewReader(zipContent.Bytes()), int64(zipContent.Len())); err != nil {
				return err
			}
			pushed[v] = true
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func visitDepthFirst(mods map[module.Version]*moduleContent, v module.Version, f func(module.Version, *moduleContent) error) error {
	m := mods[v]
	if m == nil {
		return fmt.Errorf("no module found for version %v", v)
	}
	for _, depv := range m.modFile.DepVersions() {
		if err := visitDepthFirst(mods, depv, f); err != nil {
			return err
		}
	}
	return f(v, m)
}

type Registry struct {
	srv  *httptest.Server
	host string
}

func (r *Registry) Close() {
	r.srv.Close()
}

// Host returns the hostname for the registry server;
// for example localhost:13455.
//
// The connection can be assumed to be insecure.
func (r *Registry) Host() string {
	return r.host
}

type handler struct {
	modules []*moduleContent
}

func getModules(fsys fs.FS) (map[module.Version]*moduleContent, error) {
	ctx := cuecontext.New()
	modules := make(map[string]*moduleContent)
	if err := fs.WalkDir(fsys, "_registry", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// If a filesystem has no _registry directory at all,
			// return zero modules without an error.
			if path == "_registry" && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil // we're only interested in regular files, not their parent directories
		}
		modver, rest, ok := strings.Cut(strings.TrimPrefix(path, "_registry/"), "/")
		if !ok {
			return fmt.Errorf("_registry should only contain directories, but found regular file %q", path)
		}
		content := modules[modver]
		if content == nil {
			content = &moduleContent{}
			modules[modver] = content
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		content.files = append(content.files, txtar.File{
			Name: rest,
			Data: data,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	for modver, content := range modules {
		if err := content.init(ctx, modver); err != nil {
			return nil, fmt.Errorf("cannot initialize module %q: %v", modver, err)
		}
	}
	byVer := map[module.Version]*moduleContent{}
	for _, m := range modules {
		byVer[m.version] = m
	}
	return byVer, nil
}

type moduleContent struct {
	version module.Version
	files   []txtar.File
	modFile *modfile.File
}

func (c *moduleContent) writeZip(w io.Writer) error {
	return zip.Create[txtar.File](w, c.version, c.files, txtarFileIO{})
}

func (c *moduleContent) init(ctx *cue.Context, versDir string) error {
	found := false
	for _, f := range c.files {
		if f.Name != "cue.mod/module.cue" {
			continue
		}
		modf, err := modfile.Parse(f.Data, f.Name)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("multiple module.cue files")
		}
		modp, _, ok := module.SplitPathVersion(modf.Module)
		if !ok {
			return fmt.Errorf("module %q does not contain major version", modf.Module)
		}
		mod := strings.ReplaceAll(modp, "/", "_") + "_"
		vers := strings.TrimPrefix(versDir, mod)
		if len(vers) == len(versDir) {
			return fmt.Errorf("module path %q in module.cue does not match directory %q", modf.Module, versDir)
		}
		v, err := module.NewVersion(modf.Module, vers)
		if err != nil {
			return fmt.Errorf("cannot make module version: %v", err)
		}
		c.version = v
		c.modFile = modf
		found = true
	}
	if !found {
		return fmt.Errorf("no module.cue file found in %q", versDir)
	}
	return nil
}
