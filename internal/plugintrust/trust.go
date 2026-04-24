package plugintrust

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/mod/semver"
)

//go:embed schema.cue
var schemaData string

const (
	configFileName = "plugin-trust.cue"
	schemaFile     = "cuelang.org/go/internal/plugintrust/schema.cue"
)

// Checker holds the parsed trust configuration.
// Rules are evaluated in order; the first matching rule determines the effect.
// If no rule matches, trust is denied.
type Checker struct {
	rules []rule
}

// DenialError records why a particular reference was denied.
type DenialError struct {
	Ref         goplugin.ResolvedReference
	RuleIndex   int    // -1 if denied by default policy
	Description string // from the matching rule, or "default policy"
}

func (e *DenialError) Error() string {
	return fmt.Sprintf("untrusted plugin reference to %s.%s (from %s): denied by %s",
		e.Ref.GoImportPath, e.Ref.Name, e.Ref.CUEModule, e.Description)
}

type rule struct {
	effect      string // "allow" or "deny"
	cueModule   *string
	cueModVer   *versionMatcher
	goModule    *string
	goModVer    *versionMatcher
	description string
}

type versionMatcher struct {
	raw   string
	empty bool // when true, matches only empty version strings
	comps []comparison
}

type comparison struct {
	op  string // "~", "=", ">=", ">", "<=", "<"
	ver string // canonical semver version
}

type configData struct {
	Rules []ruleData `json:"rules"`
}

type ruleData struct {
	Effect           string  `json:"effect"`
	CUEModule        *string `json:"cueModule"`
	CUEModuleVersion *string `json:"cueModuleVersion"`
	GoModule         *string `json:"goModule"`
	GoModuleVersion  *string `json:"goModuleVersion"`
	Description      string  `json:"description"`
}

// ConfigPath returns the path to the plugin trust configuration file.
func ConfigPath(getenv func(string) string) (string, error) {
	configDir, err := cueconfig.ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, configFileName), nil
}

// ReadConfig reads and parses the trust configuration from the user's config directory.
// If the file does not exist, it returns a Checker that denies all references.
func ReadConfig(getenv func(string) string) (*Checker, error) {
	p, err := ConfigPath(getenv)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Checker{}, nil
		}
		return nil, fmt.Errorf("reading %s: %v", p, err)
	}
	return ParseConfig(data, p)
}

// ParseConfig parses and validates trust configuration from CUE data.
func ParseConfig(data []byte, filename string) (*Checker, error) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(schemaData, cue.Filename(schemaFile))
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("internal error: invalid trust config schema: %v", err)
	}
	v := ctx.CompileBytes(data, cue.Filename(filename))
	if err := v.Err(); err != nil {
		return nil, err
	}
	v = v.Unify(schema.LookupPath(cue.MakePath(cue.Def("#Config"))))
	if err := v.Validate(); err != nil {
		return nil, err
	}
	var cfg configData
	if err := v.Decode(&cfg); err != nil {
		return nil, err
	}
	return buildChecker(cfg)
}

func buildChecker(cfg configData) (*Checker, error) {
	var rules []rule
	for i, rd := range cfg.Rules {
		r, err := buildRule(rd)
		if err != nil {
			return nil, fmt.Errorf("rules[%d]: %v", i, err)
		}
		rules = append(rules, r)
	}
	return &Checker{rules: rules}, nil
}

func buildRule(rd ruleData) (rule, error) {
	r := rule{
		effect:      rd.Effect,
		description: rd.Description,
		cueModule:   rd.CUEModule,
		goModule:    rd.GoModule,
	}
	if r.cueModule != nil {
		if err := validatePathPattern(*r.cueModule); err != nil {
			return rule{}, fmt.Errorf("\"cueModule\": %v", err)
		}
	}
	if rd.CUEModuleVersion != nil {
		var err error
		r.cueModVer, err = parseVersionMatcher(*rd.CUEModuleVersion)
		if err != nil {
			return rule{}, fmt.Errorf("\"cueModuleVersion\": %v", err)
		}
	}
	if r.goModule != nil {
		if err := validatePathPattern(*r.goModule); err != nil {
			return rule{}, fmt.Errorf("\"goModule\": %v", err)
		}
	}
	if rd.GoModuleVersion != nil {
		var err error
		r.goModVer, err = parseVersionMatcher(*rd.GoModuleVersion)
		if err != nil {
			return rule{}, fmt.Errorf("\"goModuleVersion\": %v", err)
		}
	}
	return r, nil
}

// Check evaluates a single resolved reference against the trust rules.
// It returns nil if the reference is allowed, or a *DenialError describing why it was denied.
func (c *Checker) Check(ref goplugin.ResolvedReference) error {
	for i := range c.rules {
		r := &c.rules[i]
		if !r.matches(ref) {
			continue
		}
		if r.effect == "allow" {
			return nil
		}
		desc := r.description
		if desc == "" {
			desc = fmt.Sprintf("rule %d", i+1)
		}
		return &DenialError{
			Ref:         ref,
			RuleIndex:   i,
			Description: desc,
		}
	}
	return &DenialError{
		Ref:         ref,
		RuleIndex:   -1,
		Description: "default policy",
	}
}

func (r *rule) matches(ref goplugin.ResolvedReference) bool {
	if r.cueModule != nil && !matchPath(*r.cueModule, ref.CUEModule.BasePath()) {
		return false
	}
	if r.cueModVer != nil && !r.cueModVer.match(ref.CUEModule.Version()) {
		return false
	}
	if r.goModule != nil && !matchPath(*r.goModule, ref.GoModule.Path) {
		return false
	}
	if r.goModVer != nil {
		if ref.GoModule.Dir != "" {
			return false
		}
		if !r.goModVer.match(ref.GoModule.Version) {
			return false
		}
	}
	return true
}

func matchPath(pattern, path string) bool {
	if pattern == "..." {
		return true
	}
	return matchElems(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// TODO looks like this is at least O(n^2) and probably worse,
// but it could be linear.
func matchElems(pattern, path []string) bool {
	for len(pattern) > 0 && len(path) > 0 {
		if pattern[0] == "..." {
			if matchElems(pattern[1:], path) {
				return true
			}
			return matchElems(pattern, path[1:])
		}
		if pattern[0] != path[0] {
			return false
		}
		pattern = pattern[1:]
		path = path[1:]
	}
	if len(pattern) == 0 {
		return len(path) == 0
	}
	for _, p := range pattern {
		if p != "..." {
			return false
		}
	}
	return true
}

func validatePathPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty path pattern")
	}
	for _, elem := range strings.Split(pattern, "/") {
		if elem == "" {
			return fmt.Errorf("empty path element in %q", pattern)
		}
		if elem != "..." && strings.Contains(elem, "...") {
			return fmt.Errorf("\"...\" must be a complete path element in %q", pattern)
		}
	}
	return nil
}

func parseVersionMatcher(s string) (*versionMatcher, error) {
	vm := &versionMatcher{raw: s}
	if s == "" {
		vm.empty = true
		return vm, nil
	}
	for _, part := range strings.Fields(s) {
		c, err := parseComparison(part)
		if err != nil {
			return nil, err
		}
		vm.comps = append(vm.comps, c)
	}
	return vm, nil
}

func parseComparison(s string) (comparison, error) {
	var op, ver string
	switch {
	case strings.HasPrefix(s, ">="):
		op, ver = ">=", s[2:]
	case strings.HasPrefix(s, "<="):
		op, ver = "<=", s[2:]
	case strings.HasPrefix(s, ">"):
		op, ver = ">", s[1:]
	case strings.HasPrefix(s, "<"):
		op, ver = "<", s[1:]
	case strings.HasPrefix(s, "~"):
		op, ver = "~", s[1:]
	case strings.HasPrefix(s, "="):
		op, ver = "=", s[1:]
	default:
		return comparison{}, fmt.Errorf("version constraint %q: operator prefix required (use =, >=, >, <=, <, or ~)", s)
	}
	if !semver.IsValid(ver) {
		return comparison{}, fmt.Errorf("invalid semver version %q in constraint %q", ver, s)
	}
	return comparison{op: op, ver: semver.Canonical(ver)}, nil
}

func (vm *versionMatcher) match(version string) bool {
	if vm.empty {
		return version == ""
	}
	for _, c := range vm.comps {
		if !c.match(version) {
			return false
		}
	}
	return true
}

func (c comparison) match(version string) bool {
	r := semver.Compare(version, c.ver)
	switch c.op {
	case "=":
		return r == 0
	case ">=":
		return r >= 0
	case ">":
		return r > 0
	case "<=":
		return r <= 0
	case "<":
		return r < 0
	case "~":
		return r >= 0 && semver.Compare(version, nextMajorVersion(c.ver)) < 0
	}
	return false
}

func nextMajorVersion(v string) string {
	major := semver.Major(v)
	n, _ := strconv.Atoi(major[1:])
	return fmt.Sprintf("v%d.0.0", n+1)
}
