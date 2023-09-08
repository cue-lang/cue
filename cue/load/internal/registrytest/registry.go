package registrytest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
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
// _registry path inside ar. The protocol that it serves is that of the
// Go proxy, documented here: https://go.dev/ref/mod#goproxy-protocol
//
// Each module should be inside a directory named path_vers, where
// slashes in path have been replaced with underscores and should
// contain a cue.mod/module.cue file holding the module info.
//
// The Registry should be closed after use.
func New(ar *txtar.Archive) (*Registry, error) {
	srv := httptest.NewServer(ociserver.New(ocimem.New(), nil))
	client, err := modregistry.NewClient(srv.URL, "cue/")
	if err != nil {
		return nil, fmt.Errorf("cannot make client: %v", err)
	}
	mods, err := getModules(ar)
	if err != nil {
		return nil, fmt.Errorf("invalid modules: %v", err)
	}
	if err := pushContent(client, mods); err != nil {
		return nil, fmt.Errorf("cannot push modules: %v", err)
	}
	return &Registry{
		srv: srv,
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

//
//func putModule(t *testing.T, c *Client, mv module.Version, content *txtar.Archive) []byte {
//	var zipContent bytes.Buffer
//	err := modzip.Create[txtar.File](&zipContent, mv, content.Files, txtarFileIO{})
//	qt.Assert(t, qt.IsNil(err))
//	zipData := zipContent.Bytes()
//	err = c.PutModule(context.Background(), mv, bytes.NewReader(zipData), int64(len(zipData)))
//	qt.Assert(t, qt.IsNil(err))
//	return zipData
//}

type Registry struct {
	srv *httptest.Server
}

func (r *Registry) Close() {
	r.srv.Close()
}

// URL returns the base URL for the registry.
func (r *Registry) URL() string {
	return r.srv.URL
}

type handler struct {
	modules []*moduleContent
}

func getModules(ar *txtar.Archive) (map[module.Version]*moduleContent, error) {
	ctx := cuecontext.New()
	modules := make(map[string]*moduleContent)
	for _, f := range ar.Files {
		path := strings.TrimPrefix(f.Name, "_registry/")
		if len(path) == len(f.Name) {
			continue
		}
		modver, rest, ok := strings.Cut(path, "/")
		if !ok {
			return nil, fmt.Errorf("_registry should only contain directories, but found regular file %q", path)
		}
		content := modules[modver]
		if content == nil {
			content = &moduleContent{}
			modules[modver] = content
		}
		content.files = append(content.files, txtar.File{
			Name: rest,
			Data: f.Data,
		})
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
