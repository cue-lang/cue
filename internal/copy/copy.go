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

// Package copy provides utilities to copy files and directories.
package copy

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// File creates dst and copies the contents src to it.
func File(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	stat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("copy file: %v", err)
	}
	err = copyFile(stat, src, dst)
	if err != nil {
		return fmt.Errorf("copy file: %v", err)
	}
	return nil
}

// Dir copies a src directory to its destination.
func Dir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	stat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("copy failed: %v", err)
	} else if !stat.IsDir() {
		return fmt.Errorf("copy failed: source is not a directory")
	}

	err = copyDir(stat, src, dst)
	if err != nil {
		return fmt.Errorf("copy failed: %v", err)
	}
	return nil
}

func copyDir(info os.FileInfo, src, dst string) error {
	if _, err := os.Stat(dst); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("dest err %s: %v", dst, err)
		}
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return fmt.Errorf("making dest %s: %v", dst, err)
		}
	}

	entries, err := ioutil.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading dir %s: %v", src, err)
	}

	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		switch mode := e.Mode(); mode & os.ModeType {
		case os.ModeSymlink:
			err = copySymLink(e, srcPath, dstPath)
		case os.ModeDir:
			err = copyDir(e, srcPath, dstPath)
		default:
			err = copyFile(e, srcPath, dstPath)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func copySymLink(info os.FileInfo, src, dst string) error {
	mode := info.Mode()
	link, err := os.Readlink(src)
	if err != nil {
		return err
	}
	err = os.Symlink(link, dst)
	if err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func copyFile(info os.FileInfo, src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("error creating %s: %v", dst, err)
	}
	defer func() {
		cErr := out.Close()
		if err == nil {
			err = cErr
		}
	}()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("error copying %s: %v", dst, err)
	}
	return err
}
