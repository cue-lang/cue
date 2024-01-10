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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
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
		// TODO: this command is still experimental, don't show it in
		// the documentation just yet.
		Hidden: true,

		Use:   "login [registry]",
		Short: "log into a CUE registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Log into a CUE registry via the OAuth 2.0 Device Authorization Grant.
Without an argument, CUE_REGISTRY is used if it points to a single registry.

Once the authorization is successful, a token is stored in a cue/logins.json file
inside your user's config directory, such as $XDG_CONFIG_HOME or %AppData%.
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

			oauthCfg := registryOAuthConfig(registry)

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

			loginsPath, err := findLoginsPath()
			if err != nil {
				return fmt.Errorf("cannot find the path to store CUE registry logins: %v", err)
			}
			logins, err := readLogins(loginsPath)
			if err != nil {
				return fmt.Errorf("cannot load CUE registry logins: %v", err)
			}

			logins.Registries[registry] = loginFromToken(tok)

			if err := writeLogins(loginsPath, logins); err != nil {
				return fmt.Errorf("cannot store CUE registry logins: %v", err)
			}
			fmt.Printf("Login for %s stored in %s\n", registry, loginsPath)
			// TODO: Once we support encryption, we should print a warning if it's not available.
			return nil
		}),
	}
	return cmd
}

func registryOAuthConfig(host string) oauth2.Config {
	// For now, we use the OAuth endpoints as implemented by registry.cue.works,
	// but other OCI registries may support the OAuth device flow with different ones.
	//
	// TODO: Query /.well-known/oauth-authorization-server to obtain
	// token_endpoint and device_authorization_endpoint per the Oauth RFCs:
	// * https://datatracker.ietf.org/doc/html/rfc8414#section-3
	// * https://datatracker.ietf.org/doc/html/rfc8628#section-4
	return oauth2.Config{
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: "https://" + host + "/login/device/code",
			TokenURL:      "https://" + host + "/login/oauth/token",
		},
	}
}

// TODO: Encrypt the JSON file if the system has a secret store available,
// such as libsecret on Linux. Such secret stores tend to have low size limits,
// so rather than store the entire JSON blob there, store an encryption key.
// There are a number of Go packages which integrate with multiple OS keychains.
//
// The encrypted form of logins.json can be logins.json.enc, for example.
// If a user has an existing logins.json file and encryption is available,
// we should replace the file with logins.json.enc transparently.

// TODO: When running "cue login", try to prevent overwriting concurrent changes
// when writing to the file on disk. For example, grab a lock, or check if the size
// changed between reading and writing the file.

func findLoginsPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "cue", "logins.json"), nil
}

func readLogins(path string) (*cueLogins, error) {
	logins := &cueLogins{
		// Initialize the map so we can insert entries.
		Registries: map[string]cueRegistryLogin{},
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return logins, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(body, logins); err != nil {
		return nil, err
	}
	return logins, nil
}

func writeLogins(path string, logins *cueLogins) error {
	// Indenting and a trailing newline are not necessary, but nicer to humans.
	body, err := json.MarshalIndent(logins, "", "\t")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	// Discourage other users from reading this file.
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return nil
}

type cueLogins struct {
	// TODO: perhaps add a version string to simplify making changes in the future

	// TODO: Sooner or later we will likely need more than one token per registry,
	// such as when our central registry starts using scopes.

	Registries map[string]cueRegistryLogin `json:"registries"`
}

type cueRegistryLogin struct {
	// These fields mirror [oauth2.Token].
	// We don't directly reference the type so we can be in control of our file format.
	// Note that Expiry is a pointer, so omitempty can work as intended.

	AccessToken string `json:"access_token"`

	TokenType string `json:"token_type,omitempty"`

	RefreshToken string `json:"refresh_token,omitempty"`

	Expiry *time.Time `json:"expiry,omitempty"`
}

func loginFromToken(tok *oauth2.Token) cueRegistryLogin {
	login := cueRegistryLogin{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
	}
	if !tok.Expiry.IsZero() {
		login.Expiry = &tok.Expiry
	}
	return login
}

func tokenFromLogin(login cueRegistryLogin) *oauth2.Token {
	tok := &oauth2.Token{
		AccessToken:  login.AccessToken,
		TokenType:    login.TokenType,
		RefreshToken: login.RefreshToken,
	}
	if login.Expiry != nil {
		tok.Expiry = *login.Expiry
	}
	return tok
}
