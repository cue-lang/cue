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

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

func newLoginCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// TODO: this command is still experimental, don't show it in
		// the documentation just yet.
		Hidden: true,

		Use:   "login [registry]",
		Short: "log into a CUE registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Log into a CUE registry via the OAuth Device Authorization Flow.
Without an argument, CUE_REGISTRY is used if it points to a single registry.
`,
		Args: cobra.MaximumNArgs(1),
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			ctx := context.Background()

			// TODO: deduplicate some of this logic with getRegistry;
			// note that ParseCUERegistry returns a Resolver, which doesn't expose the list of registries.
			// We also want to verify that a registry string is valid.
			var registry string
			if len(args) == 1 {
				registry = args[0]
				if registry == "" {
					// An explicit argument must be valid.
					return fmt.Errorf("need a CUE registry to log into")
				}
			} else {
				registry = os.Getenv("CUE_REGISTRY")
				if registry == "" {
					// CUE_REGISTRY defaults to the central registry.
					registry = "registry.cue.works"
				}
			}
			if strings.Contains(registry, ",") {
				return fmt.Errorf("need a single CUE registry to log into")
			}

			// For now, we use the endpoints as impl
			// TODO: Query /.well-known/oauth-authorization-server to obtain
			// token_endpoint and device_authorization_endpoint per the Oauth RFCs:
			// * https://datatracker.ietf.org/doc/html/rfc8414#section-3
			// * https://datatracker.ietf.org/doc/html/rfc8628#section-4
			oauthCfg := oauth2.Config{
				Endpoint: oauth2.Endpoint{
					DeviceAuthURL: "https://" + registry + "/login/device/code",
					TokenURL:      "https://" + registry + "/login/oauth/token",
				},
			}

			resp, err := oauthCfg.DeviceAuth(ctx)
			if err != nil {
				return err
			}
			// TODO: we could try using $BROWSER or xdg-open here,
			// falling back to the text instructions below
			fmt.Printf("Enter the code %s via: %s\n", resp.UserCode, resp.VerificationURI)
			fmt.Printf("Or just open: %s\n", resp.VerificationURIComplete)
			fmt.Println()
			tok, err := oauthCfg.DeviceAccessToken(ctx, resp)
			if err != nil {
				return err
			}
			// TODO: persist the access token for future use.
			fmt.Printf("Access token: %s\n", tok.AccessToken)
			return nil
		}),
	}
	return cmd
}
