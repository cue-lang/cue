package registrytest

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/module"
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
			r, err := New(ar)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			runTest(t, r.URL(), string(ar.Comment), ar)
		})
	}
}

func runTest(t *testing.T, registry string, script string, ar *txtar.Archive) {
	ctx := context.Background()
	client, err := modregistry.NewClient(registry, "cue/")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(script, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		args := strings.Fields(line)
		if len(args) == 0 || args[0] == "" {
			t.Fatalf("invalid line %q", line)
		}
		switch args[0] {
		case "modfile":
			if len(args) != 3 {
				t.Fatalf("usage: getmod $version $wantFile")
			}
			mv, err := module.ParseVersion(args[1])
			if err != nil {
				t.Fatalf("invalid version %q in getmod", args[1])
			}
			m, err := client.GetModule(ctx, mv)
			if err != nil {
				t.Fatal(err)
			}
			gotData, err := m.ModuleFile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			wantData, err := getFile(ar, args[2])
			if err != nil {
				t.Fatalf("cannot open file for body comparison: %v", err)
			}
			if string(gotData) != string(wantData) {
				t.Errorf("unexpected GET response\ngot %q\nwant %q", gotData, wantData)
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
