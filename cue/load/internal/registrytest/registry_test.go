package registrytest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

func TestRegistry(t *testing.T) {
	const testDir = "testdata"
	files, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, fi := range files {
		name := fi.Name()
		if fi.IsDir() || filepath.Ext(name) != ".txtar" {
			continue
		}
		ar, err := txtar.ParseFile(filepath.Join(testDir, fi.Name()))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(strings.TrimSuffix(name, ".txtar"), func(t *testing.T) {
			r := New(ar)
			defer r.Close()
			runTest(t, r.URL(), string(ar.Comment), ar)
		})
	}
}

func runTest(t *testing.T, registry string, script string, ar *txtar.Archive) {
	var resp *http.Response
	var respBody []byte
	for _, line := range strings.Split(script, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		args := strings.Fields(line)
		if len(args) == 0 || args[0] == "" {
			t.Fatalf("invalid line %q", line)
		}
		switch args[0] {
		case "GET":
			if len(args) != 2 {
				t.Fatalf("usage: GET $url")
			}
			resp1, err := http.Get(registry + "/" + args[1])
			if err != nil {
				t.Fatalf("GET failed: %v", err)
			}
			respBody, _ = io.ReadAll(resp1.Body)
			resp1.Body.Close()
			resp = resp1
		case "body":
			if len(args) != 3 {
				t.Fatalf("usage: body code file")
			}
			wantCode, err := strconv.Atoi(args[1])
			if err != nil {
				t.Fatalf("invalid code %q", args[1])
			}
			wantBody, err := getFile(ar, args[2])
			if err != nil {
				t.Fatalf("cannot open file for body comparison: %v", err)
			}
			if resp == nil {
				t.Fatalf("no previous GET request to check body against")
			}
			if resp.StatusCode != wantCode {
				t.Errorf("unexpected GET response code; got %v want %v", wantCode, resp.StatusCode)
			}
			if string(respBody) != string(wantBody) {
				t.Errorf("unexpected GET response\ngot %q\nwant %q", respBody, wantBody)
			}
		default:
			t.Fatalf("unknown command %q", line)
		}
	}
}

func getFile(ar *txtar.Archive, name string) ([]byte, error) {
	name = path.Clean(name)
	for _, f := range ar.Files {
		if path.Clean(f.Name) == name {
			return f.Data, nil
		}
	}
	return nil, fmt.Errorf("file %q not found in txtar archive", name)
}
