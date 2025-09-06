package modregistrytest

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocifilter"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

func TestRegistry(t *testing.T) {
	const testDir = "testdata"
	files, err := os.ReadDir(testDir)
	qt.Assert(t, qt.IsNil(err))

	for _, fi := range files {
		name := fi.Name()
		if fi.IsDir() || filepath.Ext(name) != ".txtar" {
			continue
		}
		ar, err := txtar.ParseFile(filepath.Join(testDir, fi.Name()))
		qt.Assert(t, qt.IsNil(err))
		t.Run(strings.TrimSuffix(name, ".txtar"), func(t *testing.T) {
			tfs, err := txtar.FS(ar)
			qt.Assert(t, qt.IsNil(err))
			r, err := New(tfs, "someprefix/other")
			qt.Assert(t, qt.IsNil(err))
			defer r.Close()
			client, err := ociclient.New(r.Host(), &ociclient.Options{
				Insecure: true,
			})
			qt.Assert(t, qt.IsNil(err))
			runTest(t, ocifilter.Sub(client, "someprefix/other"), string(ar.Comment), ar)
		})
	}
}

func runTest(t *testing.T, registry ociregistry.Interface, script string, ar *txtar.Archive) {
	ctx := context.Background()
	client := modregistry.NewClient(registry)
	for line := range strings.SplitSeq(script, "\n") {
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
