// Copyright 2019 CUE Authors
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

// Package source contains utility functions that standardize reading source
// bytes across cue packages.
package source

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"strings"
)

type OpenFn func(name string) (fs.File, error)

var OsOpen = func(name string) (fs.File, error) { return os.Open(name) }

// ReadAll loads the source bytes for the given arguments. If src != nil,
// ReadAll converts src to a []byte if possible; otherwise it returns an
// error. If src == nil, ReadAll returns the result of reading the file
// specified by filename.
func ReadAll(filename string, src any) ([]byte, error) {
	if src != nil {
		switch src := src.(type) {
		case string:
			return []byte(src), nil
		case []byte:
			return src, nil
		case *bytes.Buffer:
			// is io.Reader, but src is already available in []byte form
			return src.Bytes(), nil
		case io.Reader:
			return io.ReadAll(src)
		}
		return nil, fmt.Errorf("invalid source type %T", src)
	}
	return os.ReadFile(filename)
}

// ReadAllSize is like [io.ReadAll] while taking advantage of a size hint for the input reader,
// much like [os.ReadFile] does when reading regular files with a known size.
// When the size hint is negative, it simply uses [io.ReadAll].
func ReadAllSize(r io.Reader, size int) ([]byte, error) {
	if size >= 0 {
		// We use a [bytes.Buffer] here, because the given size is a hint,
		// and not guaranteed to be exactly correct.
		//
		// Before each read, [bytes.Buffer] ensures that the internal buffer
		// has enough available capacity to read at least [bytes.MinRead] bytes.
		// Many readers tend to signal EOF via a final (0, EOF) read,
		// which then triggers growing the slice to accomodate [bytes.MinRead].
		buf := bytes.NewBuffer(make([]byte, 0, size+bytes.MinRead))
		_, err := buf.ReadFrom(r)
		return buf.Bytes(), err
	}
	return io.ReadAll(r)
}

// Open creates a source reader for the given arguments.
// If src != nil, Open converts src to an [io.Reader] if possible; otherwise it returns an error.
// If src == nil, Open returns the result of [os.Open] using filename.
//
// The caller must check if the result is an [io.Closer], and if so, close it when done.
// The size of the opened reader is returned if possible, or -1 otherwise.
func Open(filename string, src any) (_ io.Reader, size int, _ error) {
	return OpenFunc(filename, src, OsOpen)
}

// OpenFunc creates a source reader for the given arguments.
// If src != nil, Open converts src to an [io.Reader] if possible; otherwise it returns an error.
// If src == nil, Open returns the result of openFn using filename.
//
// The caller must check if the result is an [io.Closer], and if so, close it when done.
// The size of the opened reader is returned if possible, or -1 otherwise.
func OpenFunc(filename string, src any, openFn OpenFn) (_ io.Reader, size int, _ error) {
	if src != nil {
		switch src := src.(type) {
		case string:
			return strings.NewReader(src), len(src), nil
		case []byte:
			return bytes.NewReader(src), len(src), nil
		case *os.File:
			return fileWithSize(src)
		case io.Reader:
			return src, -1, nil
		}
		return nil, -1, fmt.Errorf("invalid source type %T", src)
	}
	f, err := openFn(filename)
	if err != nil {
		return nil, -1, err
	}
	return fileWithSize(f)
}

func fileWithSize(f fs.File) (io.Reader, int, error) {
	// If we just opened a regular file, return its size too.
	// If we can't get its size, such as non-regular files, don't give one.
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return f, -1, nil
	}
	size := stat.Size()
	// If the size would overflow an int, it won't fit in memory anyway.
	if size > math.MaxInt {
		return f, -1, nil
	}
	return f, int(size), nil
}
