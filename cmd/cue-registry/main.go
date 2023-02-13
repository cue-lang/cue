package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/zip"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

var (
	addrFlag   = flag.String("addr", ":0", "TCP address to listen on")
	modDirFlag = flag.String("d", ".", "directory containing modules")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: cue-registry [flags]\n")
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
	}
	if err := serve(); err != nil {
		fmt.Fprintf(os.Stderr, "cue-registry: %v\n", err)
		os.Exit(1)
	}
}

func serve() error {
	h, err := newHandler(*modDirFlag)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", *addrFlag)
	if err != nil {
		return err
	}
	defer lis.Close()
	fmt.Printf("serving on %v\n", lis.Addr())
	return http.Serve(lis, h)
}

type handler struct {
	ctx     *cue.Context
	dir     string
	mu      sync.Mutex
	modules map[module.Version]*moduleContent
}

func newHandler(dir string) (*handler, error) {
	dirContents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	h := &handler{
		modules: make(map[module.Version]*moduleContent),
		ctx:     cuecontext.New(),
	}
	for _, f := range dirContents {
		if !f.IsDir() {
			return nil, fmt.Errorf("non-directory %q found in %q", f.Name(), dir)
		}
		if err := h.addModule(filepath.Join(dir, f.Name())); err != nil {
			return nil, fmt.Errorf("invalid module found: %v", err)
		}
	}
	return h, nil
}

func (h *handler) addModule(moddir string) error {
	modFilePath := filepath.Join(moddir, "cue.mod", "module.cue")
	data, err := os.ReadFile(modFilePath)
	if err != nil {
		return fmt.Errorf("cannot read module file %q: %w", modFilePath, err)
	}
	mod, err := modulePathFromModFile(h.ctx, data)
	if err != nil {
		return fmt.Errorf("invalid module file in %q: %v", modFilePath, err)
	}
	basePrefix := strings.ReplaceAll(mod, "/", "_") + "_"
	base := filepath.Base(moddir)
	vers := strings.TrimPrefix(base, basePrefix)
	if len(vers) == len(base) {
		return fmt.Errorf("module path %q in module.cue does not match directory %q", mod, moddir)
	}
	if !semver.IsValid(vers) {
		return fmt.Errorf("module version %q in %v is not valid", vers, moddir)
	}
	v := module.Version{
		Path:    mod,
		Version: vers,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.modules[v] = &moduleContent{
		dir:     moddir,
		version: v,
		modFile: data,
	}
	return nil
}

func (h *handler) modPath(v module.Version) string {
	return filepath.Join(h.dir, filepath.FromSlash(strings.ReplaceAll(v.Path, "/", "_")+"_"+v.Version))
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
		data, err := r.getModFile(mreq)
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

func (r *handler) getModFile(req *request) ([]byte, error) {
	for _, m := range r.modules {
		if m.version == req.version {
			return m.modFile, nil
		}
	}
	return nil, fmt.Errorf("no module found for %v", req.version)
}

func (h *handler) getMod(v module.Version) (*moduleContent, bool) {
	h.mu.Lock()
	m, ok := h.modules[v]
	h.mu.Unlock()
	if ok {
		return m, true
	}
	// It might have been added since we started the server.
	if err := h.addModule(h.modPath(v)); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("cannot read module from %q: %v", h.modPath(v), err)
		}
		return nil, false
	}
	return h.getMod(v)
}

func (r *handler) getZip(req *request) ([]byte, error) {
	m, ok := r.getMod(req.version)
	if !ok {
		return nil, fmt.Errorf("no module found for %v", req.version)
	}
	// TODO write this to somewhere else temporary before
	// writing to HTTP response?
	var buf bytes.Buffer
	if err := m.writeZip(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type moduleContent struct {
	dir     string
	version module.Version
	modFile []byte
}

func (c *moduleContent) writeZip(w io.Writer) error {
	return zip.CreateFromDir(w, c.version, c.dir)
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
