// Copyright 2023 The CUE Authors
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

package e2e_test

import (
	"bytes"
	cryptorand "crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	cachedGobin := os.Getenv("CUE_CACHED_GOBIN")
	if cachedGobin == "" {
		// Install the cmd/cue version into a cached GOBIN so we can reuse it.
		// TODO: use "go tool cue" once we can rely on Go 1.22's tool dependency tracking in go.mod.
		// See: https://go.dev/issue/48429
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			panic(err)
		}
		cachedGobin = filepath.Join(cacheDir, "cue-e2e-gobin")
		cmd := exec.Command("go", "install", "cuelang.org/go/cmd/cue")
		cmd.Env = append(cmd.Environ(), "GOBIN="+cachedGobin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			panic(fmt.Errorf("%v: %s", err, out))
		}
		os.Setenv("CUE_CACHED_GOBIN", cachedGobin)
	}

	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": func() int {
			// Note that we could avoid this wrapper entirely by setting PATH,
			// since TestMain sets up a single cue binary in a GOBIN directory,
			// but that may change at any point, or we might just switch to "go tool cue".
			cmd := exec.Command(filepath.Join(cachedGobin, "cue"), os.Args[1:]...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				if err, ok := err.(*exec.ExitError); ok {
					return err.ExitCode()
				}
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		},
	}))
}

var (
	// githubPublicRepo is a GitHub public repository
	// with the "cue.works authz" GitHub App installed.
	// The repository can be entirely empty, as it's only needed for authz.
	githubPublicRepo = envOr("GITHUB_PUBLIC_REPO", "github.com/cue-labs-modules-testing/e2e-public")

	// githubPublicRepo is a GitHub private repository
	// with the "cue.works authz" GitHub App installed.
	// The repository can be entirely empty, as it's only needed for authz.
	githubPrivateRepo = envOr("GITHUB_PRIVATE_REPO", "github.com/cue-labs-modules-testing/e2e-private")

	// gcloudRegistry is an existing Google Cloud Artifact Registry repository
	// to publish module versions to via "cue mod publish",
	// and authenticated via gcloud's configuration in the host environment.
	gcloudRegistry = envOr("GCLOUD_REGISTRY", "europe-west1-docker.pkg.dev/project-unity-377819/modules-e2e-registry")
)

func TestScript(t *testing.T) {
	p := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		RequireExplicitExec: true,
		Setup: func(env *testscript.Env) error {
			env.Setenv("CUE_EXPERIMENT", "modules")
			env.Setenv("CUE_REGISTRY", "registry.cue.works")
			env.Setenv("CUE_CACHED_GOBIN", os.Getenv("CUE_CACHED_GOBIN"))
			env.Setenv("CUE_REGISTRY_TOKEN", os.Getenv("CUE_REGISTRY_TOKEN"))

			// Just like cmd/cue/cmd.TestScript, set up separate cache and config dirs per test.
			env.Setenv("CUE_CACHE_DIR", filepath.Join(env.WorkDir, "tmp/cachedir"))
			configDir := filepath.Join(env.WorkDir, "tmp/configdir")
			env.Setenv("CUE_CONFIG_DIR", configDir)

			// CUE_TEST_LOGINS is a secret used by the scripts publishing to registry.cue.works.
			// When unset, those tests would fail with an auth error.
			if logins := os.Getenv("CUE_TEST_LOGINS"); logins != "" {
				if err := os.MkdirAll(configDir, 0o777); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(configDir, "logins.json"), []byte(logins), 0o666); err != nil {
					return err
				}
			}
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// github-repo-module sets $MODULE to a unique nested module under the given repository path.
			"github-repo-module": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: with-github-repo <public|private>")
				}
				moduleName := testModuleName(ts)
				var repo string
				switch args[0] {
				case "public":
					repo = githubPublicRepo
				case "private":
					repo = githubPrivateRepo
				default:
					ts.Fatalf("usage: with-github-repo <public|private>")
				}
				module := path.Join(repo, moduleName)
				ts.Setenv("MODULE", module)
				ts.Logf("using module path %s", module)
			},
			// env-fill rewrites its argument files to replace any environment variable
			// references with their values, using the same algorithm as cmpenv.
			"env-fill": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) == 0 {
					ts.Fatalf("usage: env-fill args...")
				}
				for _, arg := range args {
					path := ts.MkAbs(arg)
					data := ts.ReadFile(path)
					data = tsExpand(ts, data)
					ts.Check(os.WriteFile(path, []byte(data), 0o666))
				}
			},
			// gcloud-auth-docker configures gcloud so that it uses the host's existing configuration,
			// and sets CUE_REGISTRY and CUE_REGISTRY_HOST according to gcloudRegistry.
			"gcloud-auth-docker": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) > 0 {
					ts.Fatalf("usage: gcloud-auth-docker")
				}
				// The test script needs to be able to run gcloud as a docker credential helper.
				// gcloud will be accessible via $PATH without issue, but it needs to use its host config,
				// so we pass it along as $CLOUDSDK_CONFIG to not share the host's entire $HOME.
				//
				// We assume that the host already has gcloud authorized to upload OCI artifacts,
				// via either a user account (gcloud auth login) or a service account key (gcloud auth activate-service-account).
				gcloudConfigPath, err := exec.Command("gcloud", "info", "--format=value(config.paths.global_config_dir)").Output()
				ts.Check(err)
				ts.Setenv("CLOUDSDK_CONFIG", string(bytes.TrimSpace(gcloudConfigPath)))

				// The module path can be anything we want in this case,
				// but we might as well make it unique and realistic.
				ts.Setenv("MODULE", "domain.test/"+testModuleName(ts))

				ts.Setenv("CUE_REGISTRY", gcloudRegistry)
				// TODO: reuse internal/mod/modresolve.parseRegistry, returning a Location with Host.
				gcloudRegistryHost, _, _ := strings.Cut(gcloudRegistry, "/")
				ts.Setenv("CUE_REGISTRY_HOST", gcloudRegistryHost)
			},
		},
	}
	testscript.Run(t, p)
}

func addr[T any](t T) *T { return &t }

func envOr(name, fallback string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	return fallback
}

func envMust(t *testing.T, name string) string {
	if s := os.Getenv(name); s != "" {
		return s
	}
	t.Fatalf("%s must be set", name)
	return ""
}

func tsExpand(ts *testscript.TestScript, s string) string {
	return os.Expand(s, func(key string) string {
		return ts.Getenv(key)
	})
}

// testModuleName creates a unique string without any slashes
// which can be used as the base name for a module path to publish.
//
// It has three components:
// "e2e" with the test name as a prefix, to spot which test created it,
// a timestamp in seconds, to get an idea of when the test was run,
// and a short random suffix to avoid timing collisions between machines.
func testModuleName(ts *testscript.TestScript) string {
	var randomTrailer [3]byte
	if _, err := cryptorand.Read(randomTrailer[:]); err != nil {
		panic(err) // should typically not happen
	}
	return fmt.Sprintf("%s-%s-%x", ts.Name(),
		time.Now().UTC().Format("2006.01.02-15.04.05"), randomTrailer)
}
