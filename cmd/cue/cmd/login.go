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
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/httplog"
)

// TODO: We need a testscript to cover "cue login" with its oauth2 device flow.
// Perhaps with a small net/http/httptest server to mock the basics of the oauth2 flow?
//
// It should also test edge cases like:
//  * succeed whether or not a keychain is available
//  * load either plaintext or encrypted files, preferring the encrypted one
//  * existing login entries are kept when adding a new one
//  * using the well-known endpoint to locate oauth2 endpoints
//  * obtaining a new access token when it expires via the refresh token, and store the refreshed one
//  * asking the user to re-run "cue login" if the access token expires without a refresh token
//  * registry strings with a path prefix or an insecure option
//
// We will have end-to-end tests which will cover authentication with registry.cue.works,
// but they will use an existing token stored as a secret to avoid the human device flow in "cue login".

func newLoginCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [registry]",
		Short: "log into a CUE registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Log into a CUE registry via the OAuth 2.0 Device Authorization Grant.
Without an argument, CUE_REGISTRY is used if it points to a single registry.

Once the authorization is successful, a token is stored in a logins.json file
inside $CUE_CONFIG_DIR; see 'cue help environment'.
`,
		Args: cobra.MaximumNArgs(1),
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			ctx := backgroundContext()
			// Cause the oauth2 logic to log HTTP requests when logging is enabled.
			ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
				Transport: httpTransport(),
			})
			// Elide request and response bodies because they're likely to include sensitive information.
			ctx = httplog.RedactRequestBody(ctx, "request body can contain sensitive data when logging in")
			ctx = httplog.RedactResponseBody(ctx, "response body can contain sensitive data when logging in")

			resolver, err := getRegistryResolver()
			if err != nil {
				return err
			}
			if resolver == nil {
				return fmt.Errorf("cannot log in when modules are not enabled")
			}
			registryHosts := resolver.AllHosts()
			if len(registryHosts) > 1 {
				return fmt.Errorf("need a single CUE registry to log into")
			}
			host := registryHosts[0]
			loginsPath, err := cueconfig.LoginConfigPath(os.Getenv)
			if err != nil {
				return fmt.Errorf("cannot find the path to store CUE registry logins: %v", err)
			}
			oauthCfg := cueconfig.RegistryOAuthConfig(host)

			resp, err := oauthCfg.DeviceAuth(ctx)
			if err != nil {
				return fmt.Errorf("cannot start the OAuth2 device flow: %v", err)
			}
			// TODO: we could try using $BROWSER or xdg-open here,
			// falling back to the text instructions below
			fmt.Printf("Enter the code %s via: %s\n", resp.UserCode, resp.VerificationURI)
			fmt.Printf("Or just open: %s\n", resp.VerificationURIComplete)
			fmt.Println()
			tok, err := oauthCfg.DeviceAccessToken(ctx, resp)
			if err != nil {
				return fmt.Errorf("cannot obtain the OAuth2 token: %v", err)
			}

			// For consistency, store timestamps in UTC.
			tok.Expiry = tok.Expiry.UTC()
			// OAuth2 measures expiry in seconds via the expires_in JSON wire format field,
			// so any sub-second units add unnecessary verbosity.
			tok.Expiry = tok.Expiry.Truncate(time.Second)

			_, err = cueconfig.UpdateRegistryLogin(loginsPath, host.Name, tok)
			if err != nil {
				return fmt.Errorf("cannot store CUE registry logins: %v", err)
			}
			fmt.Printf("Login for %s stored in %s\n", host.Name, loginsPath)
			// TODO: Once we support encryption, we should print a warning if it's not available.
			return nil
		}),
	}
	return cmd
}
