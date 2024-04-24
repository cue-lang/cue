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
	"regexp"
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
type AuthConfig struct {
	// Username and Password hold the basic auth credentials.
	// If UseTokenServer is true, these apply to the token server
	// rather than to the registry itself.
	Username string `json:"username"`
	Password string `json:"password"`

	// BearerToken holds a bearer token to use as auth.
	// If UseTokenServer is true, this applies to the token server
	// rather than to the registry itself.
	BearerToken string `json:"bearerToken"`

	// UseTokenServer starts a token server and directs client
	// requests to acquire auth tokens from that server.
	UseTokenServer bool `json:"useTokenServer"`

	// ACL holds the ACL for an authenticated client.
	// If it's nil, the user is allowed full access.
	// Note: there's only one ACL because we only
	// support a single authenticated user.
	ACL *ACL `json:"acl,omitempty"`

	// Use401InsteadOf403 causes the server to send a 401
	// response even when the credentials are present and correct.
	Use401InsteadOf403 bool `json:"always401"`
}

// ACL determines what endpoints an authenticated user can accesse
// Both Allow and Deny hold a list of regular expressions that
// are matched against an HTTP request formatted as a string:
//
//	METHOD URL_PATH
//
// For example:
//
//	GET /v2/foo/bar
type ACL struct {
	// Allow holds the list of allowed paths for a user.
	// If none match, the user is forbidden.
	Allow []string
	// Deny holds the list of denied paths for a user.
	// If any match, the user is forbidden.
	Deny []string
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
// specified authorization. See the [AuthConfig] type for
// details.
//
// The Registry should be closed after use.
func New(fsys fs.FS, prefix string) (*Registry, error) {
	r := ocimem.New()

	authConfigData, err := upload(context.Background(), ocifilter.Sub(r, prefix), fsys)
	if err != nil {
		return nil, err
	}
	var authConfig *AuthConfig
	if authConfigData != nil {
		if err := json.Unmarshal(authConfigData, &authConfig); err != nil {
			return nil, fmt.Errorf("invalid auth.json: %v", err)
		}
	}
	return NewServer(ocifilter.ReadOnly(r), authConfig)
}

// NewServer is like New except that instead of uploading
// the contents of a filesystem, it just serves the contents
// of the given registry guarded by the given auth configuration.
// If auth is nil, no authentication will be required.
func NewServer(r ociregistry.Interface, auth *AuthConfig) (*Registry, error) {
	var tokenSrv *httptest.Server
	if auth != nil && auth.UseTokenServer {
		tokenSrv = httptest.NewServer(tokenHandler(auth))
	}
	r, err := authzRegistry(auth, r)
	if err != nil {
		return nil, err
	}
	srv := httptest.NewServer(&registryHandler{
		auth:     auth,
		registry: ociserver.New(r, nil),
		tokenSrv: tokenSrv,
	})
	u, err := url.Parse(srv.URL)
	if err != nil {
		return nil, err
	}
	return &Registry{
		srv:      srv,
		host:     u.Host,
		tokenSrv: tokenSrv,
	}, nil
}

// authzRegistry wraps r by checking whether the client has authorization
// to read any given repository.
func authzRegistry(auth *AuthConfig, r ociregistry.Interface) (ociregistry.Interface, error) {
	if auth == nil {
		return r, nil
	}
	allow := func(repoName string) bool {
		return true
	}
	if auth.ACL != nil {
		allowCheck, err := regexpMatcher(auth.ACL.Allow)
		if err != nil {
			return nil, fmt.Errorf("invalid allow list: %v", err)
		}
		denyCheck, err := regexpMatcher(auth.ACL.Deny)
		if err != nil {
			return nil, fmt.Errorf("invalid deny list: %v", err)
		}
		allow = func(repoName string) bool {
			return allowCheck(repoName) && !denyCheck(repoName)
		}
	}
	return ocifilter.AccessChecker(r, func(repoName string, access ocifilter.AccessKind) (_err error) {
		if !allow(repoName) {
			if auth.Use401InsteadOf403 {
				// TODO this response should be associated with a
				// Www-Authenticate header, but this won't do that.
				// Given that the ociauth logic _should_ turn
				// this back into a 403 error again, perhaps
				// we're OK.
				return ociregistry.ErrUnauthorized
			}
			// TODO should we be a bit more sophisticated and only
			// return ErrDenied when the repository doesn't exist?
			return ociregistry.ErrDenied
		}
		return nil
	}), nil
}

func regexpMatcher(patStrs []string) (func(string) bool, error) {
	pats := make([]*regexp.Regexp, len(patStrs))
	for i, s := range patStrs {
		pat, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp in ACL: %v", err)
		}
		pats[i] = pat
	}
	return func(name string) bool {
		for _, pat := range pats {
			if pat.MatchString(name) {
				return true
			}
		}
		return false
	}, nil
}

type registryHandler struct {
	auth     *AuthConfig
	registry http.Handler
	tokenSrv *httptest.Server
}

const (
	registryAuthToken   = "ok-token-for-registrytest"
	registryUnauthToken = "unauth-token-for-registrytest"
)

func (h *registryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if h.auth == nil {
		h.registry.ServeHTTP(w, req)
		return
	}
	if h.tokenSrv == nil {
		h.serveDirectAuth(w, req)
		return
	}

	// Auth with token server.
	wwwAuth := fmt.Sprintf("Bearer realm=%q,service=registrytest", h.tokenSrv.URL)
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		w.Header().Set("Www-Authenticate", wwwAuth)
		writeError(w, ociregistry.ErrUnauthorized)
		return
	}
	kind, token, ok := strings.Cut(authHeader, " ")
	if !ok || kind != "Bearer" {
		w.Header().Set("Www-Authenticate", wwwAuth)
		writeError(w, ociregistry.ErrUnauthorized)
		return
	}
	switch token {
	case registryAuthToken:
		// User is authorized.
	case registryUnauthToken:
		writeError(w, ociregistry.ErrDenied)
		return
	default:
		// If we don't recognize the token, then presumably
		// the client isn't authenticated so it's 401 not 403.
		w.Header().Set("Www-Authenticate", wwwAuth)
		writeError(w, ociregistry.ErrUnauthorized)
		return
	}
	// If the underlying registry returns a 401 error,
	// we need to add the Www-Authenticate header.
	// As there's no way to get ociserver to do it,
	// we hack it by wrapping the ResponseWriter
	// with an implementation that does.
	h.registry.ServeHTTP(&authHeaderWriter{
		wwwAuth:        wwwAuth,
		ResponseWriter: w,
	}, req)
}

func (h *registryHandler) serveDirectAuth(w http.ResponseWriter, req *http.Request) {
	auth := req.Header.Get("Authorization")
	if auth == "" {
		if h.auth.BearerToken != "" {
			// Note that this lacks information like the realm,
			// but we don't need it for our test cases yet.
			w.Header().Set("Www-Authenticate", "Bearer service=registry")
		} else {
			w.Header().Set("Www-Authenticate", "Basic service=registry")
		}
		writeError(w, fmt.Errorf("%w: no credentials", ociregistry.ErrUnauthorized))
		return
	}
	if h.auth.BearerToken != "" {
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token != h.auth.BearerToken {
			writeError(w, fmt.Errorf("%w: invalid bearer credentials", ociregistry.ErrUnauthorized))
			return
		}
	} else {
		username, password, ok := req.BasicAuth()
		if !ok || username != h.auth.Username || password != h.auth.Password {
			writeError(w, fmt.Errorf("%w: invalid user-password credentials", ociregistry.ErrUnauthorized))
			return
		}
	}
	h.registry.ServeHTTP(w, req)
}

func tokenHandler(*AuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "only POST supported", http.StatusMethodNotAllowed)
			return
		}
		req.ParseForm()
		if req.Form.Get("service") != "registrytest" {
			http.Error(w, "invalid service", http.StatusBadRequest)
			return
		}
		if req.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "invalid grant type", http.StatusBadRequest)
			return
		}
		refreshToken := req.Form.Get("refresh_token")
		if refreshToken != "registrytest-refresh" {
			http.Error(w, fmt.Sprintf("invalid refresh token %q", refreshToken), http.StatusForbidden)
			return
		}
		// See ociauth.wireToken for the full JSON format.
		data, _ := json.Marshal(map[string]string{
			"token": registryAuthToken,
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
}

// authnHandler wraps the given handler with logic that checks
// that the incoming requests fulfil the authenticiation requirements defined
// in cfg. If cfg is nil or there are no auth requirements, it returns handler
// unchanged.
func authnHandler(cfg *AuthConfig, handler http.Handler) http.Handler {
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
			writeError(w, fmt.Errorf("%w: no credentials", ociregistry.ErrUnauthorized))
			return
		}
		if cfg.BearerToken != "" {
			token, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok || token != cfg.BearerToken {
				writeError(w, fmt.Errorf("%w: invalid bearer credentials", ociregistry.ErrUnauthorized))
				return
			}
		} else {
			username, password, ok := req.BasicAuth()
			if !ok || username != cfg.Username || password != cfg.Password {
				writeError(w, fmt.Errorf("%w: invalid user-password credentials", ociregistry.ErrUnauthorized))
				return
			}
		}
		handler.ServeHTTP(w, req)
	})
}

func writeError(w http.ResponseWriter, err error) {
	data, httpStatus := ociregistry.MarshalError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	w.Write(data)
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
	srv      *httptest.Server
	tokenSrv *httptest.Server
	host     string
}

func (r *Registry) Close() {
	r.srv.Close()
	if r.tokenSrv != nil {
		r.tokenSrv.Close()
	}
}

type authHeaderWriter struct {
	wwwAuth string
	http.ResponseWriter
}

func (w *authHeaderWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized && w.Header().Get("Www-Authenticate") == "" {
		w.Header().Set("Www-Authenticate", w.wwwAuth)
	}
	w.ResponseWriter.WriteHeader(code)
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
