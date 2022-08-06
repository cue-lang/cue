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
	"errors"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

type Source interface {
	// Read loads the source bytes for the given arguments.
	// Read converts src to a []byte if possible; otherwise it returns an
	// error.
	Read() ([]byte, error)

	Reader() (io.ReadCloser, error)
}

type StringSource struct {
	src string
}

func NewStringSource(src string) *StringSource {
	return &StringSource{
		src: src,
	}
}

func (ss *StringSource) Read() ([]byte, error) {
	return []byte(ss.src), nil
}

func (ss *StringSource) Reader() (io.ReadCloser, error) {
	return ioutil.NopCloser(strings.NewReader(ss.src)), nil
}

type BytesSource struct {
	src []byte
}

func NewBytesSource(src []byte) *BytesSource {
	return &BytesSource{
		src: src,
	}
}

func (bs *BytesSource) Read() ([]byte, error) {
	return bs.src, nil
}

func (bs *BytesSource) Reader() (io.ReadCloser, error) {
	return ioutil.NopCloser(bytes.NewReader(bs.src)), nil
}

type BytesBufferSource struct {
	src *bytes.Buffer
}

func NewBytesBufferSource(src *bytes.Buffer) *BytesBufferSource {
	return &BytesBufferSource{
		src: src,
	}
}
func (bbs *BytesBufferSource) Read() ([]byte, error) {
	if bbs.src == nil {
		return nil, errors.New("BytesBufferSource is nil")
	}
	return bbs.src.Bytes(), nil
}

func (bbs *BytesBufferSource) Reader() (io.ReadCloser, error) {
	if bbs.src == nil {
		return nil, errors.New("BytesBufferSource is nil")
	}
	return ioutil.NopCloser(bytes.NewReader(bbs.src.Bytes())), nil
}

type ReaderSource struct {
	src io.Reader
}

func NewReaderSource(src io.Reader) *ReaderSource {
	return &ReaderSource{
		src: src,
	}
}

func (rs *ReaderSource) Read() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rs.src); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (rs *ReaderSource) Reader() (io.ReadCloser, error) {
	return ioutil.NopCloser(rs.src), nil
}

type FileSource struct {
	src string // filename
}

func NewFileSource(src string) *FileSource {
	return &FileSource{
		src: src,
	}
}
func (fs *FileSource) Read() ([]byte, error) {
	return ioutil.ReadFile(fs.src)
}

func (fs *FileSource) Reader() (io.ReadCloser, error) {
	return os.Open(fs.src)
}
