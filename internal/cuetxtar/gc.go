//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/txtar"
)

func main() {
	h := &handler{
		references: make(map[string]map[string]bool),
	}
	srv := httptest.NewServer(h)
	os.Setenv("CUETXTAR_GC_URI", srv.URL+"/ref")

	cmd := exec.Command("go", "test", "-count=1", "cuelang.org/go/...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for txtarFile, refs := range h.references {
		hasDiff := false
		for name := range refs {
			if strings.HasPrefix(name, "diff/") && !strings.HasPrefix(name, "diff/todo/") {
				hasDiff = true
				break
			}
		}
		a, err := txtar.ParseFile(txtarFile)
		if err != nil {
			log.Fatalf("error parsing txtar file: %v", err)
		}
		files := slices.DeleteFunc(a.Files, func(f txtar.File) bool {
			if isOutputFile(f.Name) && !refs[f.Name] {
				// Unreferenced output file.
				return true
			}
			if strings.HasPrefix(f.Name, "diff/todo/") && !hasDiff {
				// A diff-related TODO file when there are no diffs present.
				return true
			}
			return false
		})
		if len(files) == len(a.Files) {
			continue
		}
		fmt.Printf("garbage collecting %d entries from %s\n", len(a.Files)-len(files), txtarFile)
		a.Files = files
		if err := os.WriteFile(txtarFile, txtar.Format(a), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

func isOutputFile(name string) bool {
	return strings.HasPrefix(name, "out/") || (strings.HasPrefix(name, "diff/") && name != "diff/explanation" && !strings.HasPrefix(name, "diff/todo/"))
}

type handler struct {
	mu         sync.Mutex
	references map[string]map[string]bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" || req.URL.Path != "/ref" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body struct {
		TxtarFile   string   `json:"txtarfile"`
		RetainFiles []string `json:"retainFiles"`
	}
	data, _ := io.ReadAll(req.Body)
	if err := json.Unmarshal(data, &body); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	refs := h.references[body.TxtarFile]
	if refs == nil {
		refs = make(map[string]bool)
		h.references[body.TxtarFile] = refs
	}
	for _, name := range body.RetainFiles {
		refs[name] = true
	}
}
