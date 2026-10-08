// Package cueconfig holds internal API relating to CUE configuration.
package cueconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rogpeppe/go-internal/lockedfile"
	"golang.org/x/oauth2"

	"cuelang.org/go/internal/mod/modresolve"
	"cuelang.org/go/internal/robustio"
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
	// Note that we store Expiry at rest as an absolute timestamp in UTC,
	// rather than the ExpiresIn field following the RFC's wire format,
	// a duration in seconds relative to the current time which is not useful at rest.

	AccessToken string `json:"access_token"`

	TokenType string `json:"token_type,omitempty"`

	RefreshToken string `json:"refresh_token,omitempty"`

	Expiry time.Time `json:"expiry,omitzero"`
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
		return "", fmt.Errorf("cannot determine system config directory (consider setting $CUE_CONFIG_DIR): %v", err)
	}
	return filepath.Join(dir, "cue"), nil
}

func CacheDir(getenv func(string) string) (string, error) {
	if dir := getenv("CUE_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine system cache directory (consider setting $CUE_CACHE_DIR): %v", err)
	}
	return filepath.Join(dir, "cue"), nil
}

// readFile is a variable so that tests can intercept reads.
var readFile = robustio.ReadFile

func ReadLogins(path string) (*Logins, error) {
	// Hold a shared lock while reading, as writers replace the file
	// while holding the lock exclusively. On Windows, a read racing with
	// a rename can spuriously fail, and so can a rename over an open file.
	// Opening the lock file read-only creates nothing. If that fails for any
	// reason, such as no writer having run yet, fall back to reading lock-free.
	if lock, err := lockedfile.Open(path + ".lock"); err == nil {
		defer lock.Close()
	}
	return readLogins(path)
}

// readLogins reads logins.json without taking the lock file.
func readLogins(path string) (*Logins, error) {
	// Prevent ephemeral errors on Windows from other processes
	// such as antivirus software, which do not take the lock file.
	body, err := readFile(path)
	if err != nil {
		return nil, err
	}
	logins := &Logins{}
	if err := json.Unmarshal(body, logins); err != nil {
		return nil, err
	}
	// Initialize the map so we can insert entries, even if the file
	// omitted the field or set it to null.
	if logins.Registries == nil {
		logins.Registries = map[string]RegistryLogin{}
	}
	// Sanity-check the read data.
	for regName, regLogin := range logins.Registries {
		if regLogin.AccessToken == "" {
			return nil, fmt.Errorf("invalid %s: missing access_token for registry %s", path, regName)
		}
	}
	return logins, nil
}

func WriteLogins(path string, logins *Logins) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}

	unlock, err := lockedfile.MutexAt(path + ".lock").Lock()
	if err != nil {
		return err
	}
	defer unlock()

	return writeLoginsUnlocked(path, logins)
}

func writeLoginsUnlocked(path string, logins *Logins) error {
	// Indenting and a trailing newline are not necessary, but nicer to humans.
	body, err := json.MarshalIndent(logins, "", "\t")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	// Write to a temp file and rename it over the original, so that a crash
	// cannot leave a partial file. The rename is atomic for concurrent readers
	// on POSIX, but not on Windows, which is why ReadLogins takes the lock.
	if err := os.WriteFile(path+".tmp", body, 0o600); err != nil {
		return err
	}
	if err := robustio.Rename(path+".tmp", path); err != nil {
		return err
	}

	return nil
}

// UpdateRegistryLogin atomically updates a single registry token in the logins.json file.
func UpdateRegistryLogin(path string, key string, new *oauth2.Token) (*Logins, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return nil, err
	}

	unlock, err := lockedfile.MutexAt(path + ".lock").Lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	// We already hold the lock, so ReadLogins would deadlock taking it again.
	logins, err := readLogins(path)
	if errors.Is(err, fs.ErrNotExist) {
		// No config file yet; create an empty one.
		logins = &Logins{Registries: make(map[string]RegistryLogin)}
	} else if err != nil {
		return nil, err
	}

	logins.Registries[key] = LoginFromToken(new)

	err = writeLoginsUnlocked(path, logins)
	if err != nil {
		return nil, err
	}

	return logins, nil
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
	return &oauth2.Token{
		AccessToken:  login.AccessToken,
		TokenType:    login.TokenType,
		RefreshToken: login.RefreshToken,
		Expiry:       login.Expiry,
	}
}

func LoginFromToken(tok *oauth2.Token) RegistryLogin {
	return RegistryLogin{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}
}
