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
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v56/github"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/retry"
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
	// githubOrg is a GitHub organization where the "CUE Module Publisher"
	// GitHub App has been installed on all repositories.
	// This is necessary since we will create a new repository per test,
	// and there's no way to easily install the app on each repo via the API.
	githubOrg = envOr("GITHUB_ORG", "cue-labs-modules-testing")
	// githubKeep leaves the newly created repo around when set to true.
	githubKeep = envOr("GITHUB_KEEP", "false")

	// gcloudRegistry is an existing Google Cloud Artifact Registry repository
	// to upload module versions to via "cue mod upload",
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
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// create-github-repo creates a unique repository under githubOrg
			// and sets $MODULE to its resulting module path.
			"create-github-repo": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) > 0 {
					ts.Fatalf("usage: create-github-repo")
				}

				// githubToken should have read and write access to repository
				// administration and contents within githubOrg,
				// to be able to create repositories under the org and git push to them.
				// Not a global, since
				githubToken := envMust(t, "GITHUB_TOKEN")

				repoName := testModuleName(ts)
				client := github.NewClient(nil).WithAuthToken(githubToken)
				ctx := context.TODO()

				repo := &github.Repository{
					Name: github.String(repoName),
				}
				_, _, err := client.Repositories.Create(ctx, githubOrg, repo)
				ts.Check(err)

				// Unless GITHUB_KEEP=true is set, delete the repo when we finish.
				//
				// TODO: It might be useful to leave the repo around when the test fails.
				// We would need testscript.TestScript to expose T.Failed for this.
				ts.Defer(func() {
					if githubKeep == "true" {
						return
					}
					_, err := client.Repositories.Delete(ctx, githubOrg, repoName)
					ts.Check(err)
				})

				ts.Setenv("MODULE", fmt.Sprintf("github.com/%s/%s", githubOrg, repoName))
				ts.Setenv("GITHUB_TOKEN", githubToken) // needed for "git push"
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
			// cue-mod-wait waits for a CUE module to exist in a registry for up to 20s.
			// Since this is easily done via an HTTP HEAD request, an OCI client isn't necessary.
			"cue-mod-wait": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) > 0 {
					ts.Fatalf("usage: cue-mod-wait")
				}
				manifest := tsExpand(ts, "https://${CUE_REGISTRY}/v2/${MODULE}/manifests/${VERSION}")
				retries := retry.Strategy{
					Delay:       10 * time.Millisecond,
					MaxDelay:    time.Second,
					MaxDuration: 20 * time.Second,
				}
				for it := retries.Start(); it.Next(nil); {
					resp, err := http.Head(manifest)
					ts.Check(err)
					if resp.StatusCode == http.StatusOK {
						return
					}
				}
				ts.Fatalf("timed out waiting for module")
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
// which can be used as the base name for a module path to publish,
// so that test runs don't conflict with one another
// and can be easily attributed to a point in time.
func testModuleName(ts *testscript.TestScript) string {
	// TODO: name the repo after ts.Name once the API lands
	// TODO: add a short random suffix to prevent time collisions
	return time.Now().UTC().Format("2006-01-02.15-04-05")
}
