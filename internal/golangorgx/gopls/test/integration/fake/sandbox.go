// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fake

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/internal/robustio"
	"golang.org/x/tools/txtar"
)

// Sandbox holds a collection of temporary resources to use for working with Go
// code in tests.
type Sandbox struct {
	rootdir string
	Workdir *Workdir
}

// SandboxConfig controls the behavior of a test sandbox. The zero value
// defines a reasonable default.
type SandboxConfig struct {
	// RootDir sets the base directory to use when creating temporary
	// directories. If not specified, defaults to a new temporary directory.
	RootDir string
	// Files holds a txtar-encoded archive of files to populate the initial state
	// of the working directory.
	//
	// For convenience, the special substring "$SANDBOX_WORKDIR" is replaced with
	// the sandbox's resolved working directory before writing files.
	Files map[string][]byte
	// Workdir configures the working directory of the Sandbox. It behaves as
	// follows:
	//  - if set to an absolute path, use that path as the working directory.
	//  - if set to a relative path, create and use that path relative to the
	//    sandbox.
	//  - if unset, default to a the 'work' subdirectory of the sandbox.
	Workdir string
}

// NewSandbox creates a collection of named temporary resources, with a
// working directory populated by the txtar-encoded content in srctxt, and a
// file-based module proxy populated with the txtar-encoded content in
// proxytxt.
//
// If rootDir is non-empty, it will be used as the root of temporary
// directories created for the sandbox. Otherwise, a new temporary directory
// will be used as root.
//
// TODO(rfindley): the sandbox abstraction doesn't seem to carry its weight.
// Sandboxes should be composed out of their building-blocks, rather than via a
// monolithic configuration.
func NewSandbox(config *SandboxConfig) (_ *Sandbox, err error) {
	if config == nil {
		config = new(SandboxConfig)
	}
	if err := validateConfig(*config); err != nil {
		return nil, fmt.Errorf("invalid SandboxConfig: %v", err)
	}

	sb := &Sandbox{}
	defer func() {
		// Clean up if we fail at any point in this constructor.
		if err != nil {
			sb.Close()
		}
	}()

	rootDir := config.RootDir
	if rootDir == "" {
		rootDir, err = os.MkdirTemp(config.RootDir, "cue-lsp-sandbox-")
		if err != nil {
			return nil, fmt.Errorf("creating temporary workdir: %v", err)
		}
	}
	sb.rootdir = rootDir
	// Short-circuit writing the workdir if we're given an absolute path, since
	// this is used for running in an existing directory.
	// TODO(findleyr): refactor this to be less of a workaround.
	if filepath.IsAbs(config.Workdir) {
		sb.Workdir, err = NewWorkdir(config.Workdir, nil)
		if err != nil {
			return nil, err
		}
		return sb, nil
	}
	var workdir string
	if config.Workdir == "" {
		if workdir == "" {
			workdir = filepath.Join(sb.rootdir, "work")
		}
	} else {
		// relative path
		workdir = filepath.Join(sb.rootdir, config.Workdir)
	}
	if err := os.MkdirAll(workdir, 0755); err != nil {
		return nil, err
	}
	sb.Workdir, err = NewWorkdir(workdir, config.Files)
	if err != nil {
		return nil, err
	}
	return sb, nil
}

// Tempdir creates a new temp directory with the given txtar-encoded files. It
// is the responsibility of the caller to call os.RemoveAll on the returned
// file path when it is no longer needed.
func Tempdir(files map[string][]byte) (string, error) {
	dir, err := os.MkdirTemp("", "cue-lsp-tempdir-")
	if err != nil {
		return "", err
	}
	for name, data := range files {
		if err := writeFileData(name, data, RelativeTo(dir)); err != nil {
			return "", fmt.Errorf("writing to tempdir: %w", err)
		}
	}
	return dir, nil
}

func UnpackTxt(txt string) map[string][]byte {
	dataMap := make(map[string][]byte)
	archive := txtar.Parse([]byte(txt))
	for _, f := range archive.Files {
		if _, ok := dataMap[f.Name]; ok {
			panic(fmt.Sprintf("found file %q twice", f.Name))
		}
		dataMap[f.Name] = f.Data
	}
	return dataMap
}

func validateConfig(config SandboxConfig) error {
	if filepath.IsAbs(config.Workdir) && len(config.Files) > 0 {
		return errors.New("absolute Workdir cannot be set in conjunction with Files")
	}
	return nil
}

// splitModuleVersionPath extracts module information from files stored in the
// directory structure modulePath@version/suffix.
// For example:
//
//	splitModuleVersionPath("mod.com@v1.2.3/package") = ("mod.com", "v1.2.3", "package")
func splitModuleVersionPath(path string) (modulePath, version, suffix string) {
	parts := strings.Split(path, "/")
	var modulePathParts []string
	for i, p := range parts {
		if strings.Contains(p, "@") {
			mv := strings.SplitN(p, "@", 2)
			modulePathParts = append(modulePathParts, mv[0])
			return strings.Join(modulePathParts, "/"), mv[1], strings.Join(parts[i+1:], "/")
		}
		modulePathParts = append(modulePathParts, p)
	}
	// Default behavior: this is just a module path.
	return path, "", ""
}

func (sb *Sandbox) RootDir() string {
	return sb.rootdir
}

// Close removes all state associated with the sandbox.
func (sb *Sandbox) Close() error {
	err := robustio.RemoveAll(sb.rootdir)
	if err != nil {
		return fmt.Errorf("error(s) cleaning sandbox: removing files: %v", err)
	}
	return nil
}
