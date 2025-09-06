// Copyright 2024 CUE Authors
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

package modresolve

import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net"
	"net/netip"
	"path"
	"slices"
	"strings"
	"sync"

	"cuelabs.dev/go/oci/ociregistry/ociref"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/mod/module"
)

// pathEncoding represents one of the possible types of
// encoding for module paths within a registry.
// It reflects the #registry.pathEncoding disjunction
// in schema.cue.
// TODO it would be nice if this could be auto-generated
// from the schema.
type pathEncoding string

const (
	encPath       pathEncoding = "path"
	encHashAsRepo pathEncoding = "hashAsRepo"
	encHashAsTag  pathEncoding = "hashAsTag"
)

// LocationResolver resolves module paths to a location
// consisting of a host name of a registry and where
// in that registry the module is to be found.
//
// Note: The implementation in this package operates entirely lexically,
// which is why [Location] contains only a host name and not an actual
// [ociregistry.Interface] implementation.
type LocationResolver interface {
	// ResolveToLocation resolves a base module path (without a version
	// suffix, a.k.a. OCI repository name) and optional version to
	// the location for that path. It reports whether it can find
	// appropriate location for the module.
	//
	// If the version is empty, the Tag in the returned Location
	// will hold the prefix that all versions of the module in its
	// repository have. That prefix will be followed by the version
	// itself.
	ResolveToLocation(path string, vers string) (Location, bool)

	// AllHosts returns all the registry hosts that the resolver
	// might resolve to, ordered lexically by hostname.
	AllHosts() []Host
}

// Host represents a registry host name.
type Host struct {
	// Name holds the IP host name of the registry.
	// If it's an IP v6 address, it will be surrounded with
	// square brackets ([, ]).
	Name string
	// Insecure holds whether this host should be connected
	// to insecurely (with an HTTP rather than HTTP connection).
	Insecure bool
}

// Location represents the location for a given module version or versions.
type Location struct {
	// Host holds the host or host:port of the registry.
	Host string

	// Insecure holds whether an insecure connection
	// should be used when connecting to the registry.
	Insecure bool

	// Repository holds the repository to store the module in.
	Repository string

	// Tag holds the tag for the module version.
	// If an empty version was passed to
	// Resolve, it holds the prefix shared by all version
	// tags for the module.
	Tag string
}

// config mirrors the #File definition in schema.cue.
// TODO it would be nice to be able to generate this
// type directly from the schema.
type config struct {
	ModuleRegistries map[string]*registryConfig `json:"moduleRegistries,omitempty"`
	DefaultRegistry  *registryConfig            `json:"defaultRegistry,omitempty"`
}

func (cfg *config) init() error {
	for prefix, reg := range cfg.ModuleRegistries {
		if err := module.CheckPathWithoutVersion(prefix); err != nil {
			return fmt.Errorf("invalid module path %q: %v", prefix, err)
		}
		if err := reg.init(); err != nil {
			return fmt.Errorf("invalid registry configuration in %q: %v", prefix, err)
		}
	}
	if cfg.DefaultRegistry != nil {
		if err := cfg.DefaultRegistry.init(); err != nil {
			return fmt.Errorf("invalid default registry configuration: %v", err)
		}
	}
	return nil
}

type registryConfig struct {
	Registry      string       `json:"registry,omitempty"`
	PathEncoding  pathEncoding `json:"pathEncoding,omitempty"`
	PrefixForTags string       `json:"prefixForTags,omitempty"`
	StripPrefix   bool         `json:"stripPrefix,omitempty"`

	// The following fields are filled in from Registry after parsing.
	none       bool
	host       string
	repository string
	insecure   bool
}

func (r *registryConfig) init() error {
	r1, err := parseRegistry(r.Registry)
	if err != nil {
		return err
	}
	r.none, r.host, r.repository, r.insecure = r1.none, r1.host, r1.repository, r1.insecure

	if r.PrefixForTags != "" {
		if !ociref.IsValidTag(r.PrefixForTags) {
			return fmt.Errorf("invalid tag prefix %q", r.PrefixForTags)
		}
	}
	if r.PathEncoding == "" {
		// Shouldn't happen because default should apply.
		return fmt.Errorf("empty pathEncoding")
	}
	if r.StripPrefix {
		if r.PathEncoding != encPath {
			// TODO we could relax this to allow storing of naked tags
			// when the module path matches exactly and hash tags
			// otherwise.
			return fmt.Errorf("cannot strip prefix unless using path encoding")
		}
		if r.repository == "" {
			return fmt.Errorf("use of stripPrefix requires a non-empty repository within the registry")
		}
	}
	return nil
}

var (
	configSchemaOnce sync.Once // guards the creation of _configSchema
	// TODO remove this mutex when https://cuelang.org/issue/2733 is fixed.
	configSchemaMutex sync.Mutex // guards any use of _configSchema
	_configSchema     cue.Value
)

//go:embed schema.cue
var configSchemaData []byte

// RegistryConfigSchema returns the CUE schema
// for the configuration parsed by [ParseConfig].
func RegistryConfigSchema() string {
	// Cut out the copyright header and the header that's
	// not pure schema.
	schema := string(configSchemaData)
	i := strings.Index(schema, "\n// #file ")
	if i == -1 {
		panic("no file definition found in schema")
	}
	i++
	return schema[i:]
}

// ParseConfig parses the registry configuration with the given contents and file name.
// If there is no default registry, then the single registry specified in catchAllDefault
// will be used as a default.
func ParseConfig(configFile []byte, filename string, catchAllDefault string) (LocationResolver, error) {
	configSchemaOnce.Do(func() {
		ctx := cuecontext.New()
		schemav := ctx.CompileBytes(configSchemaData, cue.Filename("cuelang.org/go/internal/mod/modresolve/schema.cue"))
		schemav = schemav.LookupPath(cue.MakePath(cue.Def("#file")))
		if err := schemav.Validate(); err != nil {
			panic(fmt.Errorf("internal error: invalid CUE registry config schema: %v", errors.Details(err, nil)))
		}
		_configSchema = schemav
	})
	configSchemaMutex.Lock()
	defer configSchemaMutex.Unlock()

	v := _configSchema.Context().CompileBytes(configFile, cue.Filename(filename))
	if err := v.Err(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid registry configuration file")
	}
	v = v.Unify(_configSchema)
	if err := v.Err(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid configuration file")
	}
	var cfg config
	if err := v.Decode(&cfg); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into registry config struct")
	}
	if err := cfg.init(); err != nil {
		return nil, err
	}
	if cfg.DefaultRegistry == nil {
		if catchAllDefault == "" {
			return nil, fmt.Errorf("no default catch-all registry provided")
		}
		// TODO is it too limiting to have the catch-all registry specified as a simple string?
		reg, err := parseRegistry(catchAllDefault)
		if err != nil {
			return nil, fmt.Errorf("invalid catch-all registry %q: %v", catchAllDefault, err)
		}
		cfg.DefaultRegistry = reg
	}
	r := &resolver{
		cfg: cfg,
	}
	if err := r.initHosts(); err != nil {
		return nil, err
	}
	return r, nil
}

// ParseCUERegistry parses a registry routing specification that
// maps module prefixes to the registry that should be used to
// fetch that module.
//
// The specification consists of an order-independent, comma-separated list.
//
// Each element either maps a module prefix to the registry that will be used
// for all modules that have that prefix (prefix=registry), or a catch-all registry to be used
// for modules that do not match any prefix (registry).
//
// For example:
//
//	myorg.com=myregistry.com/m,catchallregistry.example.org
//
// Any module with a matching prefix will be routed to the given registry.
// A prefix only matches whole path elements.
// In the above example, module myorg.com/foo/bar@v0 will be looked up
// in myregistry.com in the repository m/myorg.com/foo/bar,
// whereas github.com/x/y will be looked up in catchallregistry.example.com.
//
// The registry part is syntactically similar to a [docker reference]
// except that the repository is optional and no tag or digest is allowed.
// Additionally, a +secure or +insecure suffix may be used to indicate
// whether to use a secure or insecure connection. Without that,
// localhost, 127.0.0.1 and [::1] will default to insecure, and anything
// else to secure.
//
// If s does not declare a catch-all registry location, catchAllDefault is
// used. It is an error if s fails to declares a catch-all registry location
// and no catchAllDefault is provided.
//
// [docker reference]: https://pkg.go.dev/github.com/distribution/reference
func ParseCUERegistry(s string, catchAllDefault string) (LocationResolver, error) {
	if s == "" && catchAllDefault == "" {
		return nil, fmt.Errorf("no catch-all registry or default")
	}
	if s == "" {
		s = catchAllDefault
	}
	cfg := config{
		ModuleRegistries: make(map[string]*registryConfig),
	}
	parts := strings.SplitSeq(s, ",")
	for part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			if part == "" {
				// TODO or just ignore it?
				return nil, fmt.Errorf("empty registry part")
			}
			if _, ok := cfg.ModuleRegistries[""]; ok {
				return nil, fmt.Errorf("duplicate catch-all registry")
			}
			key, val = "", part
		} else {
			if key == "" {
				return nil, fmt.Errorf("empty module prefix")
			}
			if val == "" {
				return nil, fmt.Errorf("empty registry reference")
			}
			if err := module.CheckPathWithoutVersion(key); err != nil {
				return nil, fmt.Errorf("invalid module path %q: %v", key, err)
			}
			if _, ok := cfg.ModuleRegistries[key]; ok {
				return nil, fmt.Errorf("duplicate module prefix %q", key)
			}
		}
		reg, err := parseRegistry(val)
		if err != nil {
			return nil, fmt.Errorf("invalid registry %q: %v", val, err)
		}
		cfg.ModuleRegistries[key] = reg
	}
	if _, ok := cfg.ModuleRegistries[""]; !ok {
		if catchAllDefault == "" {
			return nil, fmt.Errorf("no default catch-all registry provided")
		}
		reg, err := parseRegistry(catchAllDefault)
		if err != nil {
			return nil, fmt.Errorf("invalid catch-all registry %q: %v", catchAllDefault, err)
		}
		cfg.ModuleRegistries[""] = reg
	}
	cfg.DefaultRegistry = cfg.ModuleRegistries[""]
	delete(cfg.ModuleRegistries, "")

	r := &resolver{
		cfg: cfg,
	}
	if err := r.initHosts(); err != nil {
		return nil, err
	}
	return r, nil
}

type resolver struct {
	allHosts []Host
	cfg      config
}

func (r *resolver) initHosts() error {
	hosts := make(map[string]bool)
	addHost := func(reg *registryConfig) error {
		if reg.none {
			return nil
		}
		if insecure, ok := hosts[reg.host]; ok {
			if insecure != reg.insecure {
				return fmt.Errorf("registry host %q is specified both as secure and insecure", reg.host)
			}
		} else {
			hosts[reg.host] = reg.insecure
		}
		return nil
	}
	for _, reg := range r.cfg.ModuleRegistries {
		if err := addHost(reg); err != nil {
			return err
		}
	}

	if reg := r.cfg.DefaultRegistry; reg != nil {
		if err := addHost(reg); err != nil {
			return err
		}
	}
	allHosts := make([]Host, 0, len(hosts))
	for host, insecure := range hosts {
		allHosts = append(allHosts, Host{
			Name:     host,
			Insecure: insecure,
		})
	}
	slices.SortFunc(allHosts, func(a, b Host) int {
		return cmp.Compare(a.Name, b.Name)
	})
	r.allHosts = allHosts
	return nil
}

// AllHosts implements Resolver.AllHosts.
func (r *resolver) AllHosts() []Host {
	return r.allHosts
}

func (r *resolver) ResolveToLocation(mpath, vers string) (Location, bool) {
	if mpath == "" {
		return Location{}, false
	}
	bestMatch := ""
	// Note: there's always a wildcard match.
	bestMatchReg := r.cfg.DefaultRegistry
	for pat, reg := range r.cfg.ModuleRegistries {
		if pat == mpath {
			bestMatch = pat
			bestMatchReg = reg
			break
		}
		if !strings.HasPrefix(mpath, pat) {
			continue
		}
		if len(bestMatch) > len(pat) {
			// We've already found a more specific match.
			continue
		}
		if mpath[len(pat)] != '/' {
			// The path doesn't have a separator at the end of
			// the prefix, which means that it doesn't match.
			// For example, foo.com/bar does not match foo.com/ba.
			continue
		}
		// It's a possible match but not necessarily the longest one.
		bestMatch, bestMatchReg = pat, reg
	}
	reg := bestMatchReg
	if reg == nil || reg.none {
		return Location{}, false
	}
	loc := Location{
		Host:     reg.host,
		Insecure: reg.insecure,
		Tag:      vers,
	}
	switch reg.PathEncoding {
	case encPath:
		if reg.StripPrefix {
			mpath = strings.TrimPrefix(mpath, bestMatch)
			mpath = strings.TrimPrefix(mpath, "/")
		}
		loc.Repository = path.Join(reg.repository, mpath)
	case encHashAsRepo:
		loc.Repository = fmt.Sprintf("%s/%x", reg.repository, sha256.Sum256([]byte(mpath)))
	case encHashAsTag:
		loc.Repository = reg.repository
	default:
		panic("unreachable")
	}
	if reg.PathEncoding == encHashAsTag {
		loc.Tag = fmt.Sprintf("%s%x-%s", reg.PrefixForTags, sha256.Sum256([]byte(mpath)), vers)
	} else {
		loc.Tag = reg.PrefixForTags + vers
	}
	return loc, true
}

func parseRegistry(env0 string) (*registryConfig, error) {
	if env0 == "none" {
		return &registryConfig{
			Registry: env0,
			none:     true,
		}, nil
	}
	env := env0
	var suffix string
	if i := strings.LastIndex(env, "+"); i > 0 {
		suffix = env[i:]
		env = env[:i]
	}
	var r ociref.Reference
	if !strings.Contains(env, "/") {
		// OCI references don't allow a host name on its own without a repo,
		// but we do.
		r.Host = env
		if !ociref.IsValidHost(r.Host) {
			return nil, fmt.Errorf("invalid host name %q in registry", r.Host)
		}
	} else {
		var err error
		r, err = ociref.Parse(env)
		if err != nil {
			return nil, err
		}
		if r.Tag != "" || r.Digest != "" {
			return nil, fmt.Errorf("cannot have an associated tag or digest")
		}
	}
	if suffix == "" {
		if isInsecureHost(r.Host) {
			suffix = "+insecure"
		} else {
			suffix = "+secure"
		}
	}
	insecure := false
	switch suffix {
	case "+insecure":
		insecure = true
	case "+secure":
	default:
		return nil, fmt.Errorf("unknown suffix (%q), need +insecure, +secure or no suffix)", suffix)
	}
	return &registryConfig{
		Registry:     env0,
		PathEncoding: encPath,
		host:         r.Host,
		repository:   r.Repository,
		insecure:     insecure,
	}, nil
}

var (
	ipV4Localhost = netip.MustParseAddr("127.0.0.1")
	ipV6Localhost = netip.MustParseAddr("::1")
)

func isInsecureHost(hostPort string) bool {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		}
	}
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	// TODO other clients have logic for RFC1918 too, amongst other
	// things. Maybe we should do that too.
	return addr == ipV4Localhost || addr == ipV6Localhost
}
