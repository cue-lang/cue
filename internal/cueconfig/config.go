// Package cueconfig holds internal API relating to CUE configuration.
package cueconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cuelang.org/go/internal/mod/modresolve"
	"golang.org/x/oauth2"
)

// Logins holds the login information as stored in $CUE_CONFIG_DIR/logins.cue.
type Logins struct {
	// TODO: perhaps add a version string to simplify making changes in the future

	// TODO: Sooner or later we will likely need more than one token per registry,
	// such as when our central registry starts using scopes.

	Registries map[string]RegistryLogin `json:"registries"`
}

// RegistryLogin holds the login information for one registry.
type RegistryLogin struct {
	// These fields mirror [oauth2.Token].
	// We don't directly reference the type so we can be in control of our file format.
	// Note that Expiry is a pointer, so omitempty can work as intended.

	AccessToken string `json:"access_token"`

	TokenType string `json:"token_type,omitempty"`

	RefreshToken string `json:"refresh_token,omitempty"`

	Expiry *time.Time `json:"expiry,omitempty"`
}

func LoginConfigPath(getenv func(string) string) (string, error) {
	configDir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "logins.json"), nil
}

func ConfigDir(getenv func(string) string) (string, error) {
	if dir := getenv("CUE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine system config directory: %v", err)
	}
	return filepath.Join(dir, "cue"), nil
}

func CacheDir(getenv func(string) string) (string, error) {
	if dir := getenv("CUE_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine system cache directory: %v", err)
	}
	return filepath.Join(dir, "cue"), nil
}

func ReadLogins(path string) (*Logins, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	logins := &Logins{
		// Initialize the map so we can insert entries.
		Registries: map[string]RegistryLogin{},
	}
	if err := json.Unmarshal(body, logins); err != nil {
		return nil, err
	}
	return logins, nil
}

func WriteLogins(path string, logins *Logins) error {
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

// RegistryOAuthConfig returns the oauth2 configuration
// suitable for talking to the central registry.
func RegistryOAuthConfig(host modresolve.Host) oauth2.Config {
	// For now, we use the OAuth endpoints as implemented by registry.cue.works,
	// but other OCI registries may support the OAuth device flow with different ones.
	//
	// TODO: Query /.well-known/oauth-authorization-server to obtain
	// token_endpoint and device_authorization_endpoint per the Oauth RFCs:
	// * https://datatracker.ietf.org/doc/html/rfc8414#section-3
	// * https://datatracker.ietf.org/doc/html/rfc8628#section-4
	scheme := "https://"
	if host.Insecure {
		scheme = "http://"
	}
	return oauth2.Config{
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: scheme + host.Name + "/login/device/code",
			TokenURL:      scheme + host.Name + "/login/oauth/token",
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

func TokenFromLogin(login RegistryLogin) *oauth2.Token {
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

func LoginFromToken(tok *oauth2.Token) RegistryLogin {
	login := RegistryLogin{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
	}
	if !tok.Expiry.IsZero() {
		login.Expiry = &tok.Expiry
	}
	return login
}
