package registrytest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/zip"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
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
func New(ar *txtar.Archive) *Registry {
	h, err := newHandler(ar)
	if err != nil {
		panic(err)
	}
	srv := httptest.NewServer(h)
	return &Registry{
		srv: srv,
	}
}

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

func newHandler(ar *txtar.Archive) (*handler, error) {
	ctx := cuecontext.New()
	modules := make(map[string]*moduleContent)
	for _, f := range ar.Files {
		path := strings.TrimPrefix(f.Name, "_registry/")
		if len(path) == len(f.Name) {
			continue
		}
		modver, rest, ok := strings.Cut(path, "/")
		if !ok {
			return nil, fmt.Errorf("cannot have regular file inside _registry")
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
		if err := content.initVersion(ctx, modver); err != nil {
			return nil, fmt.Errorf("cannot determine version for module in %q: %v", modver, err)
		}
	}
	mods := make([]*moduleContent, 0, len(modules))
	for _, m := range modules {
		mods = append(mods, m)
	}
	return &handler{
		modules: mods,
	}, nil
}

var modulePath = cue.MakePath(cue.Str("module"))

func modulePathFromModFile(ctx *cue.Context, data []byte) (string, error) {
	v := ctx.CompileBytes(data)
	if err := v.Err(); err != nil {
		return "", fmt.Errorf("invalid module.cue syntax: %v", err)
	}
	v = v.LookupPath(modulePath)
	s, err := v.String()
	if err != nil {
		return "", fmt.Errorf("cannot get module value from module.cue file: %v", err)
	}
	if s == "" {
		return "", fmt.Errorf("empty module directive")
	}
	// TODO check for valid module path?
	return s, nil
}

func (r *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	mreq, err := parseReq(req.URL.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot parse request %q: %v", req.URL.Path, err), http.StatusBadRequest)
		return
	}
	switch mreq.kind {
	case reqMod:
		data, err := r.getMod(mreq)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot get module: %v", err), http.StatusNotFound)
			return
		}
		// TODO content type
		w.Write(data)
	case reqZip:
		data, err := r.getZip(mreq)
		if err != nil {
			// TODO this can fail for non-NotFound reasons too.
			http.Error(w, fmt.Sprintf("cannot get module contents: %v", err), http.StatusNotFound)
			return
		}
		// TODO content type
		w.Header().Set("Content-Type", "application/zip")
		w.Write(data)
	default:
		http.Error(w, "not implemented yet", http.StatusInternalServerError)
	}
}

func (r *handler) getMod(req *request) ([]byte, error) {
	for _, m := range r.modules {
		if m.version == req.version {
			return m.getMod(), nil
		}
	}
	return nil, fmt.Errorf("no module found for %v", req.version)
}

func (r *handler) getZip(req *request) ([]byte, error) {
	for _, m := range r.modules {
		if m.version == req.version {
			// TODO write this to somewhere else temporary before
			// writing to HTTP response.
			var buf bytes.Buffer
			if err := m.writeZip(&buf); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("no module found for %v", req.version)
}

type moduleContent struct {
	version module.Version
	files   []txtar.File
}

func (c *moduleContent) writeZip(w io.Writer) error {
	files := make([]zip.File, len(c.files))
	for i := range c.files {
		files[i] = zipFile{&c.files[i]}
	}
	return zip.Create(w, c.version, files)
}

type zipFile struct {
	f *txtar.File
}

// Path implements zip.File.Path.
func (f zipFile) Path() string {
	return f.f.Name
}

// Lstat implements zip.File.Lstat.
func (f zipFile) Lstat() (os.FileInfo, error) {
	return f, nil
}

func (f zipFile) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.f.Data)), nil
}

// Name implements fs.FileInfo.Name.
func (f zipFile) Name() string {
	return path.Base(f.f.Name)
}

// Mode implements fs.FileInfo.Mode.
func (f zipFile) Mode() os.FileMode {
	return 0
}

// Size implements fs.FileInfo.Size.
func (f zipFile) Size() int64 {
	return int64(len(f.f.Data))
}

func (f zipFile) IsDir() bool {
	return false
}
func (f zipFile) ModTime() time.Time {
	return time.Time{}
}
func (f zipFile) Sys() any {
	return nil
}

func (c *moduleContent) getMod() []byte {
	for _, f := range c.files {
		if f.Name == "cue.mod/module.cue" {
			return f.Data
		}
	}
	panic(fmt.Errorf("no module.cue file found in %v", c.version))
}

func (c *moduleContent) initVersion(ctx *cue.Context, versDir string) error {
	for _, f := range c.files {
		if f.Name != "cue.mod/module.cue" {
			continue
		}
		mod, err := modulePathFromModFile(ctx, f.Data)
		if err != nil {
			return fmt.Errorf("invalid module file in %q: %v", path.Join(versDir, f.Name), err)
		}
		if c.version.Path != "" {
			return fmt.Errorf("multiple module.cue files")
		}
		c.version.Path = mod
		mod = strings.ReplaceAll(mod, "/", "_") + "_"
		vers := strings.TrimPrefix(versDir, mod)
		if len(vers) == len(versDir) {
			return fmt.Errorf("module path %q in module.cue does not match directory %q", c.version.Path, versDir)
		}
		if !semver.IsValid(vers) {
			return fmt.Errorf("module version %q is not valid", vers)
		}
		c.version.Version = vers
	}
	if c.version.Path == "" {
		return fmt.Errorf("no module.cue file found in %q", versDir)
	}
	return nil
}

type reqKind int

const (
	reqInvalid reqKind = iota
	reqLatest
	reqList
	reqMod
	reqZip
	reqInfo
)

type request struct {
	version module.Version
	kind    reqKind
}

func parseReq(urlPath string) (*request, error) {
	urlPath = strings.TrimPrefix(urlPath, "/")
	i := strings.LastIndex(urlPath, "/@")
	if i == -1 {
		return nil, fmt.Errorf("no @ found in path")
	}
	if i == 0 {
		return nil, fmt.Errorf("empty module name in path")
	}
	var req request
	mod, rest := urlPath[:i], urlPath[i+1:]
	req.version.Path = mod
	qual, rest, ok := strings.Cut(rest, "/")
	if qual == "@latest" {
		if ok {
			return nil, fmt.Errorf("invalid @latest request")
		}
		// $base/$module/@latest
		req.kind = reqLatest
		return &req, nil
	}
	if qual != "@v" {
		return nil, fmt.Errorf("invalid @ in request")
	}
	if !ok {
		return nil, fmt.Errorf("no qualifier after @")
	}
	if rest == "list" {
		// $base/$module/@v/list
		req.kind = reqList
		return &req, nil
	}
	i = strings.LastIndex(rest, ".")
	if i == -1 {
		return nil, fmt.Errorf("no . found after @")
	}
	vers, rest := rest[:i], rest[i+1:]
	if len(vers) == 0 {
		return nil, fmt.Errorf("empty version string")
	}
	req.version.Version = vers
	switch rest {
	case "info":
		req.kind = reqInfo
	case "mod":
		req.kind = reqMod
	case "zip":
		req.kind = reqZip
	default:
		return nil, fmt.Errorf("unknown request kind %q", rest)
	}
	return &req, nil
}
