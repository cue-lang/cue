// Copyright 2026 The CUE Authors
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

// Package cuetestscript holds the testscript environment that the cue command
// expects: the module registries derived from a test's _registry* directories,
// the cache directory, the language version variables, and the registry-related
// commands that scripts use.
//
// It exists so that there is a single implementation of that contract. It is
// used by the cmd/cue tests, which run cue in process, and is also suitable for
// a host which provides the same environment to scripts that run cue as an
// external binary.
//
// Such hosts differ in how they report failure and in how they hold on to
// state, so setup steps and commands here work in terms of [Env] and [CmdEnv]
// rather than any concrete testscript type, and report command failure by
// returning an error rather than by failing a test directly.
package cuetestscript

import (
	"fmt"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ociserver"

	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modregistrytest"
)

// Env is the environment that a setup step operates in. It is implemented by
// the host: a thin wrapper around testscript's own environment when cue runs in
// process, or whatever holds a test's environment in a host which runs cue as
// an external binary.
type Env interface {
	// WorkDir returns the absolute path of the test's work directory.
	WorkDir() string

	// Getenv returns the value of the named environment variable in the test
	// environment, or the empty string if it is not set.
	Getenv(key string) string

	// Setenv sets an environment variable in the test environment. Only the
	// variables named by [ResultEnv] are set.
	Setenv(key, value string)

	// Defer registers a function to be run when the test has finished.
	Defer(f func())
}

// CmdEnv is the environment that a command runs in.
type CmdEnv interface {
	Env

	// MkAbs interprets path relative to the command's working directory.
	MkAbs(path string) string

	// Stdout and Stderr return the command's output streams.
	Stdout() io.Writer
	Stderr() io.Writer
}

// maxRegistries bounds the number of numbered registry environment variables
// (CUE_REGISTRY, CUE_REGISTRY1, ...) that [ResultEnv] declares. It corresponds
// to the number of _registry* directories a single test is expected to use.
const maxRegistries = 10

// ResultEnv returns the names of all the environment variables that [Setup] and
// the commands returned by [Cmds] may set. It is an explicit list rather than a
// wildcard so that a host which must declare what it sets can do so without
// maintaining its own copy.
func ResultEnv() []string {
	env := []string{
		"CUE_CACHE_DIR",
		"CUE_LANGUAGE_VERSION",
		"CUE_LANGUAGE_VERSION_BUGFIX",
	}
	for _, id := range registryIDs() {
		env = append(env, "CUE_REGISTRY"+id, "DEBUG_REGISTRY"+id+"_HOST")
	}
	return env
}

// registryIDs returns the registry ids used to form CUE_REGISTRY* variable
// names: the empty id (CUE_REGISTRY) plus "0" to "maxRegistries-1".
func registryIDs() []string {
	ids := []string{""}
	for i := range maxRegistries {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}

// Setup prepares the environment that the cue command expects for a single
// test: a module root above the work directory, a predictable cache directory,
// the language version variables, and a registry server for each _registry*
// directory in the work directory.
//
// Any variable it sets is named by [ResultEnv].
func Setup(e Env) error {
	if err := setupModuleRoot(e); err != nil {
		return err
	}
	// os.UserCacheDir relies on OS-specific env vars that we don't set,
	// so point the cache somewhere predictable inside the work directory.
	e.Setenv("CUE_CACHE_DIR", filepath.Join(e.WorkDir(), ".tmp/cache"))
	// The current language version which would be added by `cue mod init`,
	// e.g. v0.10.0, and a later version which only increases the bugfix
	// release, e.g. v0.10.99.
	e.Setenv("CUE_LANGUAGE_VERSION", cueversion.LanguageVersion())
	e.Setenv("CUE_LANGUAGE_VERSION_BUGFIX", semver.MajorMinor(cueversion.LanguageVersion())+".99")
	return setupRegistries(e)
}

// setupModuleRoot makes the parent of the work directory a module.
//
// If a script loads CUE packages but forgot to set up a cue.mod, we might walk
// up to the system's temporary directory looking for cue.mod. If /tmp/cue.mod
// exists, for instance, this can lead to test failures as our behavior when it
// comes to the module root and file paths changes. Creating the directory
// ensures consistent behavior no matter what parent directories contain.
//
// Note that creating the directory is enough for now, and we ignore ErrExist
// since only the first test will succeed.
func setupModuleRoot(e Env) error {
	workdirRoot := filepath.Dir(e.WorkDir())
	if err := os.Mkdir(filepath.Join(workdirRoot, "cue.mod"), 0o777); err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

// setupRegistries starts a fake registry server for each _registry* directory
// in the work directory, serving the modules found in it, and sets the
// corresponding CUE_REGISTRY* and DEBUG_REGISTRY*_HOST variables.
//
// A _registry<id>_prefix file holds a path prefix for the registry, and a
// _registry<id>_proxy file holding a boolean fronts it with an OCI proxy
// server, mirroring the way that the Central Registry works.
func setupRegistries(e Env) error {
	workDir := e.WorkDir()
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("cannot read work directory: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		regID, ok := strings.CutPrefix(entry.Name(), "_registry")
		if !ok {
			continue
		}
		prefix := ""
		if data, err := os.ReadFile(filepath.Join(workDir, "_registry"+regID+"_prefix")); err == nil {
			prefix = strings.TrimSpace(string(data))
		}
		useProxy := false
		proxyFile := filepath.Join(workDir, "_registry"+regID+"_proxy")
		if data, err := os.ReadFile(proxyFile); err == nil {
			useProxy, err = strconv.ParseBool(strings.TrimSpace(string(data)))
			if err != nil {
				return fmt.Errorf("invalid contents of proxy file %q: %v", proxyFile, err)
			}
		}
		regHost, err := startRegistry(e, os.DirFS(filepath.Join(workDir, entry.Name())), prefix, useProxy)
		if err != nil {
			return err
		}
		regPrefix := ""
		if prefix != "" {
			regPrefix = "/" + prefix
		}
		e.Setenv("CUE_REGISTRY"+regID, regHost+regPrefix+"+insecure")
		// This enables some tests to construct their own malformed
		// CUE_REGISTRY values that still refer to the test registry.
		e.Setenv("DEBUG_REGISTRY"+regID+"_HOST", regHost)
	}
	return nil
}

// startRegistry starts a registry server holding the modules in fsys, served
// under the given path prefix, registering its cleanup with e and returning its
// host. If useProxy is set, the registry is fronted by an OCI proxy server.
func startRegistry(e Env, fsys fs.FS, prefix string, useProxy bool) (string, error) {
	reg, err := modregistrytest.New(fsys, prefix)
	if err != nil {
		return "", fmt.Errorf("cannot start test registry server: %v", err)
	}
	e.Defer(reg.Close)
	host := reg.Host()
	if !useProxy {
		return host, nil
	}
	proxyClient, err := ociclient.New(host, &ociclient.Options{
		Insecure: true,
	})
	if err != nil {
		return "", fmt.Errorf("cannot create oci proxy client: %v", err)
	}
	proxy := httptest.NewServer(ociserver.New(proxyClient, nil))
	e.Defer(proxy.Close)
	proxyURL, _ := url.Parse(proxy.URL)
	return proxyURL.Host, nil
}
