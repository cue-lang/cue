// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package source

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAllTypedNil(t *testing.T) {
	// Create a temporary file to verify that typed nils cause
	// a fallback to reading from the file.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	want := []byte("file content")
	if err := os.WriteFile(path, want, 0o666); err != nil {
		t.Fatal(err)
	}

	// A nil []byte boxed into an interface{} should be treated
	// the same as an untyped nil, falling back to os.ReadFile.
	var b []byte // nil
	got, err := ReadAll(path, b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAll with nil []byte: got %q, want %q", got, want)
	}

	// A nil *bytes.Buffer should also fall back to os.ReadFile.
	var buf *bytes.Buffer // nil
	got, err = ReadAll(path, buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAll with nil *bytes.Buffer: got %q, want %q", got, want)
	}
}

func TestOpenTypedNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	want := []byte("file content")
	if err := os.WriteFile(path, want, 0o666); err != nil {
		t.Fatal(err)
	}

	// A nil []byte should fall back to os.Open.
	var b []byte // nil
	r, _, err := Open(path, b)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := r.(interface{ Close() error }); ok {
		defer c.Close()
	}
	var gotBuf bytes.Buffer
	if _, err := gotBuf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBuf.Bytes(), want) {
		t.Fatalf("Open with nil []byte: got %q, want %q", gotBuf.Bytes(), want)
	}

	// A nil *os.File should fall back to os.Open.
	var f *os.File // nil
	r, _, err = Open(path, f)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := r.(interface{ Close() error }); ok {
		defer c.Close()
	}
	gotBuf.Reset()
	if _, err := gotBuf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBuf.Bytes(), want) {
		t.Fatalf("Open with nil *os.File: got %q, want %q", gotBuf.Bytes(), want)
	}
}
