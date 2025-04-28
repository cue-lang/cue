// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gcimporter

import (
	"fmt"
	"go/token"
	"go/types"
	"io"
)

func IExportShallow(fset *token.FileSet, pkg *types.Package, reportf ReportFunc) ([]byte, error) {
	return nil, fmt.Errorf("we are not loading Go packages")
}

func IImportShallow(fset *token.FileSet, getPackages GetPackagesFunc, data []byte, path string, reportf ReportFunc) (*types.Package, error) {
	return nil, fmt.Errorf("we are not loading Go packages")
}

type GetPackagesFunc = func(items []GetPackagesItem) error

type GetPackagesItem struct {
	Name, Path string
	Pkg        *types.Package

	pathOffset uint64
	nameIndex  map[string]uint64
}

type ReportFunc = func(string, ...interface{})

func IExportData(out io.Writer, fset *token.FileSet, pkg *types.Package) error {
	return fmt.Errorf("we are not loading Go packages")
}

func IExportBundle(out io.Writer, fset *token.FileSet, pkgs []*types.Package) error {
	return fmt.Errorf("we are not loading Go packages")
}

func FindPkg(path, srcDir string) (filename, id string) {
	return
}

func Import(packages map[string]*types.Package, path, srcDir string, lookup func(path string) (io.ReadCloser, error)) (pkg *types.Package, err error) {
	return nil, fmt.Errorf("we are not loading Go packages")
}
