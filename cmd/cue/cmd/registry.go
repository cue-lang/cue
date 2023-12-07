// Copyright 2023 CUE Authors
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

package cmd

import (
	"fmt"
	"os"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociauth"
	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/modmux"
	"cuelang.org/go/internal/mod/modresolve"
)

// getRegistry returns the registry to pull modules from.
// If external modules are disabled and there's no other issue,
// it returns (nil, nil).
func getRegistry() (ociregistry.Interface, error) {
	// TODO document CUE_REGISTRY via a new "cue help environment" subcommand.
	env := os.Getenv("CUE_REGISTRY")
	if !cueexperiment.Flags.Modules {
		if env != "" {
			fmt.Fprintf(os.Stderr, "warning: ignoring CUE_REGISTRY because modules experiment is not enabled. Set CUE_EXPERIMENT=modules to enable it.\n")
		}
		return nil, nil
	}
	resolver, err := modresolve.ParseCUERegistry(env, "registry.cuelabs.dev")
	if err != nil {
		return nil, fmt.Errorf("bad value for $CUE_REGISTRY: %v", err)
	}
	// If the user isn't doing anything that requires a registry, we
	// shouldn't complain about reading a bad configuration file,
	// so check only when required.
	var auth ociauth.Authorizer
	var authErr error
	var authOnce sync.Once

	return modmux.New(resolver, func(host string, insecure bool) (ociregistry.Interface, error) {
		authOnce.Do(func() {
			config, err := ociauth.Load(nil)
			if err != nil {
				authErr = fmt.Errorf("cannot load OCI auth configuration: %v", err)
				return
			}
			auth = ociauth.NewStdAuthorizer(ociauth.StdAuthorizerParams{
				Config: config,
			})
		})
		if authErr != nil {
			return nil, authErr
		}
		return ociclient.New(host, &ociclient.Options{
			Insecure:   insecure,
			Authorizer: auth,
		})
	}), nil
}
