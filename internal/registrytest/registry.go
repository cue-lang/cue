package registrytest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ocifilter"
	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

// AuthConfig specifies authorization requirements for the server.
// Currently it only supports basic and bearer auth.
type AuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`

	BearerToken string `json:"bearerToken"`
}

// Upload uploads the modules found inside fsys (stored
// in the format described by [New]) to the given registry.
func Upload(ctx context.Context, r ociregistry.Interface, fsys fs.FS) error {
	_, err := upload(ctx, r, fsys)
	return err
}

func upload(ctx context.Context, r ociregistry.Interface, fsys fs.FS) (authConfig []byte, err error) {
	client := modregistry.NewClient(r)
	mods, authConfigData, err := getModules(fsys)
	if err != nil {
		return nil, fmt.Errorf("invalid modules: %v", err)
	}

	if err := pushContent(ctx, client, mods); err != nil {
		return nil, fmt.Errorf("cannot push modules: %v", err)
	}
	return authConfigData, nil
}

// New starts a registry instance that serves modules found inside fsys.
// It serves the OCI registry protocol.
// If prefix is non-empty, all module paths will be prefixed by that,
// separated by a slash (/).
//
// Each module should be inside a directory named path_vers, where
// slashes in path have been replaced with underscores and should
// contain a cue.mod/module.cue file holding the module info.
//
// If there's a file named auth.json in the root directory,
// it will cause access to the server to be gated by the
// specified authorization. See the AuthConfig type for
// details.
//
// The Registry should be closed after use.
func New(fsys fs.FS, prefix string) (*Registry, error) {
	handler, err := NewHandler(fsys, prefix)
	if err != nil {
		return nil, err
	}
	srv := httptest.NewServer(handler)
	u, err := url.Parse(srv.URL)
	if err != nil {
		return nil, err
	}
	return &Registry{
		srv:  srv,
		host: u.Host,
	}, nil
}

// NewHandler is similar to [New] except that it just returns
// the HTTP handler for the server instead of actually starting
// a server.
func NewHandler(fsys fs.FS, prefix string) (http.Handler, error) {
	r := ocimem.New()

	authConfigData, err := upload(context.Background(), ocifilter.Sub(r, prefix), fsys)
	if err != nil {
		return nil, err
	}
	var handler http.Handler = ociserver.New(ocifilter.ReadOnly(r), nil)
	if authConfigData != nil {
		var cfg AuthConfig
		if err := json.Unmarshal(authConfigData, &cfg); err != nil {
			return nil, fmt.Errorf("invalid auth.json: %v", err)
		}
		handler = AuthHandler(handler, &cfg)
	}
	return handler, nil
}

// AuthHandler wraps the given handler with logic that checks
// that the incoming requests fulfil the auth requirements defined
// in cfg. If cfg is nil or there are no auth requirements, it returns handler
// unchanged.
func AuthHandler(handler http.Handler, cfg *AuthConfig) http.Handler {
	if cfg == nil || (*cfg == AuthConfig{}) {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth := req.Header.Get("Authorization")
		if auth == "" {
			if cfg.BearerToken != "" {
				// Note that this lacks information like the realm,
				// but we don't need it for our test cases yet.
				w.Header().Set("Www-Authenticate", "Bearer service=registry")
			} else {
				w.Header().Set("Www-Authenticate", "Basic service=registry")
			}
			http.Error(w, "no credentials", http.StatusUnauthorized)
			return
		}
		if cfg.BearerToken != "" {
			token, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok || token != cfg.BearerToken {
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
		} else {
			username, password, ok := req.BasicAuth()
			if !ok || username != cfg.Username || password != cfg.Password {
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(w, req)
	})
}

func pushContent(ctx context.Context, client *modregistry.Client, mods map[module.Version]*moduleContent) error {
	pushed := make(map[module.Version]bool)
	// Iterate over modules in deterministic order.
	// TODO use maps.Keys when available.
	vs := make([]module.Version, 0, len(mods))
	for v := range mods {
		vs = append(vs, v)
	}
	module.Sort(vs)
	for _, v := range vs {
		err := visitDepthFirst(mods, v, func(v module.Version, m *moduleContent) error {
			if pushed[v] {
				return nil
			}
			var zipContent bytes.Buffer
			if err := m.writeZip(&zipContent); err != nil {
				return err
			}
			if err := client.PutModule(ctx, v, bytes.NewReader(zipContent.Bytes()), int64(zipContent.Len())); err != nil {
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
		return nil
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

func getModules(fsys fs.FS) (map[module.Version]*moduleContent, []byte, error) {
	var authConfig []byte
	ctx := cuecontext.New()
	modules := make(map[string]*moduleContent)
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// If a filesystem has no entries at all,
			// return zero modules without an error.
			if path == "." && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil // we're only interested in regular files, not their parent directories
		}
		if path == "auth.json" {
			authConfig, err = fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			return nil
		}
		modver, rest, ok := strings.Cut(path, "/")
		if !ok {
			return fmt.Errorf("registry should only contain directories, but found regular file %q", path)
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
		return nil, nil, err
	}
	for modver, content := range modules {
		if err := content.init(ctx, modver); err != nil {
			return nil, nil, fmt.Errorf("cannot initialize module %q: %v", modver, err)
		}
	}
	byVer := map[module.Version]*moduleContent{}
	for _, m := range modules {
		byVer[m.version] = m
	}
	return byVer, authConfig, nil
}

type moduleContent struct {
	version module.Version
	files   []txtar.File
	modFile *modfile.File
}

func (c *moduleContent) writeZip(w io.Writer) error {
	return modzip.Create[txtar.File](w, c.version, c.files, txtarFileIO{})
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
