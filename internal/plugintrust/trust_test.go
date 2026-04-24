package plugintrust

import (
	"errors"
	"testing"

	qt "github.com/go-quicktest/qt"

	"cuelang.org/go/cue/inject/goplugin"
	"cuelang.org/go/mod/module"
)

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"...", "anything", true},
		{"...", "a/b/c", true},
		{"...", "", true},
		{"example.com/foo", "example.com/foo", true},
		{"example.com/foo", "example.com/bar", false},
		{"example.com/foo", "example.com/foo/bar", false},
		{"example.com/foo", "example.com", false},
		{"example.com/...", "example.com", true},
		{"example.com/...", "example.com/foo", true},
		{"example.com/...", "example.com/foo/bar", true},
		{"example.com/...", "other.com/foo", false},
		{"example.com/.../util", "example.com/util", true},
		{"example.com/.../util", "example.com/foo/util", true},
		{"example.com/.../util", "example.com/foo/bar/util", true},
		{"example.com/.../util", "example.com/foo/bar/other", false},
		{"example.com/.../util", "example.com", false},
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b", false},
		{"a/b/c", "a/b/c/d", false},
		{".../b/c", "a/b/c", true},
		{".../b/c", "b/c", true},
		{".../b/c", "x/y/b/c", true},
		{"a/.../c", "a/c", true},
		{"a/.../c", "a/b/c", true},
		{"a/.../c", "a/b/d/c", true},
		{"a/.../c", "a/b/d/e", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			got := matchPath(tt.pattern, tt.path)
			qt.Assert(t, qt.Equals(got, tt.want))
		})
	}
}

func TestValidatePathPattern(t *testing.T) {
	tests := []struct {
		pattern string
		wantErr string
	}{
		{"example.com/foo", ""},
		{"...", ""},
		{"example.com/...", ""},
		{"example.com/.../util", ""},
		{"", "empty path pattern"},
		{"example.com//foo", "empty path element"},
		{"exam.../foo", `"..." must be a complete path element`},
		{"foo/...bar", `"..." must be a complete path element`},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			err := validatePathPattern(tt.pattern)
			if tt.wantErr == "" {
				qt.Assert(t, qt.IsNil(err))
			} else {
				qt.Assert(t, qt.ErrorMatches(err, ".*"+tt.wantErr+".*"))
			}
		})
	}
}

func TestParseComparison(t *testing.T) {
	tests := []struct {
		input   string
		wantOp  string
		wantVer string
		wantErr string
	}{
		{"=v1.2.3", "=", "v1.2.3", ""},
		{">=v1.0.0", ">=", "v1.0.0", ""},
		{">v0.1.0", ">", "v0.1.0", ""},
		{"<=v2.0.0", "<=", "v2.0.0", ""},
		{"<v1.0.0", "<", "v1.0.0", ""},
		{"~v1.2.3", "~", "v1.2.3", ""},
		{"=v1.2.3-beta.1", "=", "v1.2.3-beta.1", ""},
		{"v1.2.3", "", "", "operator prefix required"},
		{"=invalid", "", "", "invalid semver version"},
		{"", "", "", "operator prefix required"},
		{">=", "", "", "invalid semver version"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c, err := parseComparison(tt.input)
			if tt.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, ".*"+tt.wantErr+".*"))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(c.op, tt.wantOp))
			qt.Assert(t, qt.Equals(c.ver, tt.wantVer))
		})
	}
}

func TestVersionMatch(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		{"=v1.2.3", "v1.2.3", true},
		{"=v1.2.3", "v1.2.4", false},
		{"=v1.2.3", "v1.2.2", false},
		{">=v1.0.0", "v1.0.0", true},
		{">=v1.0.0", "v2.0.0", true},
		{">=v1.0.0", "v0.9.0", false},
		{">v1.0.0", "v1.0.1", true},
		{">v1.0.0", "v1.0.0", false},
		{"<=v1.0.0", "v1.0.0", true},
		{"<=v1.0.0", "v0.9.0", true},
		{"<=v1.0.0", "v1.0.1", false},
		{"<v1.0.0", "v0.9.9", true},
		{"<v1.0.0", "v1.0.0", false},
		{"~v1.2.3", "v1.2.3", true},
		{"~v1.2.3", "v1.9.0", true},
		{"~v1.2.3", "v2.0.0", false},
		{"~v1.2.3", "v1.2.2", false},
		{"~v0.3.0", "v0.3.0", true},
		{"~v0.3.0", "v0.9.9", true},
		{"~v0.3.0", "v1.0.0", false},
		{">=v1.0.0 <v1.10.0", "v1.0.0", true},
		{">=v1.0.0 <v1.10.0", "v1.9.0", true},
		{">=v1.0.0 <v1.10.0", "v1.10.0", false},
		{">=v1.0.0 <v1.10.0", "v0.9.0", false},
		// Pre-release versions
		{"=v1.0.0-beta.1", "v1.0.0-beta.1", true},
		{">=v1.0.0-beta.1", "v1.0.0-beta.2", true},
		{">=v1.0.0-beta.1", "v1.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.constraint+"_"+tt.version, func(t *testing.T) {
			vm, err := parseVersionMatcher(tt.constraint)
			qt.Assert(t, qt.IsNil(err))
			got := vm.match(tt.version)
			qt.Assert(t, qt.Equals(got, tt.want))
		})
	}
}

func TestVersionMatcherEmpty(t *testing.T) {
	vm, err := parseVersionMatcher("")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(vm.empty))
	qt.Assert(t, qt.IsTrue(vm.match("")))
	qt.Assert(t, qt.IsFalse(vm.match("v1.0.0")))
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		nRules  int
		wantErr string
	}{
		{
			name:   "empty",
			input:  "{}",
			nRules: 0,
		},
		{
			name:   "no rules field",
			input:  "",
			nRules: 0,
		},
		{
			name: "single allow rule",
			input: `rules: [{
				effect: "allow"
				cueModule: "example.com/..."
			}]`,
			nRules: 1,
		},
		{
			name: "multiple rules",
			input: `rules: [
				{effect: "deny", cueModule: "example.com/untrusted"},
				{effect: "allow", cueModule: "example.com/..."},
			]`,
			nRules: 2,
		},
		{
			name: "all fields",
			input: `rules: [{
				effect: "allow"
				cueModule: "example.com/..."
				cueModuleVersion: ">=v1.0.0 <v2.0.0"
				goModule: "github.com/..."
				goModuleVersion: "~v1.0.0"
				description: "trusted publisher"
			}]`,
			nRules: 1,
		},
		{
			name: "empty version matches main module",
			input: `rules: [{
				effect: "allow"
				cueModule: "..."
				cueModuleVersion: ""
			}]`,
			nRules: 1,
		},
		{
			name:    "missing effect",
			input:   `rules: [{cueModule: "example.com/..."}]`,
			wantErr: `effect`,
		},
		{
			name:    "invalid effect",
			input:   `rules: [{effect: "permit"}]`,
			wantErr: `effect`,
		},
		{
			name:    "unknown field in rule",
			input:   `rules: [{effect: "allow", unknown: "value"}]`,
			wantErr: `unknown`,
		},
		{
			name:    "unknown top-level field",
			input:   `rule: [{effect: "allow"}]`,
			wantErr: `rule`,
		},
		{
			name:    "invalid version constraint",
			input:   `rules: [{effect: "allow", cueModuleVersion: "v1.0.0"}]`,
			wantErr: `operator prefix required`,
		},
		{
			name:    "invalid path pattern",
			input:   `rules: [{effect: "allow", cueModule: "exam.../foo"}]`,
			wantErr: `must be a complete path element`,
		},
		{
			name:    "rules not a list",
			input:   `rules: "not a list"`,
			wantErr: `rules`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := ParseConfig([]byte(tt.input), "test.cue")
			if tt.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, ".*"+tt.wantErr+".*"))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(len(checker.rules), tt.nRules))
		})
	}
}

func TestCheckAllowAll(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "allow"
		cueModule: "..."
	}]`)
	ref := makeRef("example.com@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	qt.Assert(t, qt.IsNil(checker.Check(ref)))
}

func TestCheckDenyAll(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "deny"
		cueModule: "..."
	}]`)
	ref := makeRef("example.com@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(ref))
	qt.Assert(t, qt.Equals(de.RuleIndex, 0))
}

func TestCheckDefaultDeny(t *testing.T) {
	checker := mustParseConfig(t, `{}`)
	ref := makeRef("example.com@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(ref))
	qt.Assert(t, qt.Equals(de.RuleIndex, -1))
	qt.Assert(t, qt.Equals(de.Description, "default policy"))
}

func TestCheckFirstMatchWins(t *testing.T) {
	checker := mustParseConfig(t, `rules: [
		{effect: "deny", cueModule: "example.com/untrusted"},
		{effect: "allow", cueModule: "example.com/..."},
	]`)

	denied := makeRef("example.com/untrusted@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(denied))
	qt.Assert(t, qt.Equals(de.RuleIndex, 0))

	allowed := makeRef("example.com/trusted@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	qt.Assert(t, qt.IsNil(checker.Check(allowed)))
}

func TestCheckCUEModuleVersionConstraint(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "allow"
		cueModule: "example.com/strutil"
		cueModuleVersion: "<=v0.2.1"
	}]`)

	allowed := makeRef("example.com/strutil@v0", "v0.2.1", "github.com/pkg", "v1.0.0", "Func")
	qt.Assert(t, qt.IsNil(checker.Check(allowed)))

	denied := makeRef("example.com/strutil@v0", "v0.3.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(denied))
	qt.Assert(t, qt.Equals(de.Description, "default policy"))
}

func TestCheckGoModuleMatch(t *testing.T) {
	checker := mustParseConfig(t, `rules: [
		{effect: "allow", cueModule: "example.com/...", goModule: "github.com/Masterminds/goutils"},
		{effect: "deny", cueModule: "example.com/..."},
	]`)

	allowed := makeRef("example.com/strutil@v0", "v0.1.0", "github.com/Masterminds/goutils", "v1.5.0", "Func")
	qt.Assert(t, qt.IsNil(checker.Check(allowed)))

	denied := makeRef("example.com/strutil@v0", "v0.1.0", "github.com/other/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(denied))
	qt.Assert(t, qt.Equals(de.RuleIndex, 1))
}

func TestCheckEmptyVersionMatchesMainModule(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "allow"
		cueModule: "..."
		cueModuleVersion: ""
	}]`)

	mainMod := makeRef("mymod.example@v0", "", "github.com/pkg", "v1.0.0", "Func")
	qt.Assert(t, qt.IsNil(checker.Check(mainMod)))

	dep := makeRef("dep.example@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(dep))
	qt.Assert(t, qt.Equals(de.Description, "default policy"))
}

func TestCheckDirReplaceSkipsVersionConstraint(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "allow"
		goModule: "github.com/pkg"
		goModuleVersion: ">=v1.0.0"
	}]`)

	ref := goplugin.ResolvedReference{
		CUEModule:    module.MustNewVersion("example.com@v0", "v0.1.0"),
		GoModule:     goplugin.GoModule{Path: "github.com/pkg", Dir: "../local/pkg"},
		GoImportPath: "github.com/pkg",
		Name:         "Func",
	}
	de := checkDenied(t, checker.Check(ref))
	qt.Assert(t, qt.Equals(de.Description, "default policy"))
}

func TestCheckDirReplaceWithoutVersionConstraint(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "allow"
		goModule: "github.com/pkg"
	}]`)

	ref := goplugin.ResolvedReference{
		CUEModule:    module.MustNewVersion("example.com@v0", "v0.1.0"),
		GoModule:     goplugin.GoModule{Path: "github.com/pkg", Dir: "../local/pkg"},
		GoImportPath: "github.com/pkg",
		Name:         "Func",
	}
	qt.Assert(t, qt.IsNil(checker.Check(ref)))
}

func TestCheckNoPredicatesMatchesAll(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{effect: "allow"}]`)
	ref := makeRef("any.example@v0", "v0.1.0", "github.com/any/pkg", "v2.0.0", "AnyFunc")
	qt.Assert(t, qt.IsNil(checker.Check(ref)))
}

func TestCheckRuleDescription(t *testing.T) {
	checker := mustParseConfig(t, `rules: [{
		effect: "deny"
		cueModule: "..."
		description: "no plugins allowed in this environment"
	}]`)
	ref := makeRef("example.com@v0", "v0.1.0", "github.com/pkg", "v1.0.0", "Func")
	de := checkDenied(t, checker.Check(ref))
	qt.Assert(t, qt.Equals(de.Description, "no plugins allowed in this environment"))
}

func TestDenialErrorMessage(t *testing.T) {
	checker := mustParseConfig(t, `{}`)
	ref := makeRef("example.com/strutil@v0", "v0.0.1", "github.com/Masterminds/goutils", "v1.5.0", "Uncapitalize")
	err := checker.Check(ref)
	qt.Assert(t, qt.ErrorMatches(err, `.*untrusted plugin reference.*goutils\.Uncapitalize.*denied by default policy.*`))
}

func TestNextMajorVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v0.1.0", "v1.0.0"},
		{"v1.2.3", "v2.0.0"},
		{"v2.0.0", "v3.0.0"},
		{"v10.5.3", "v11.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nextMajorVersion(tt.input)
			qt.Assert(t, qt.Equals(got, tt.want))
		})
	}
}

func mustParseConfig(t *testing.T, input string) *Checker {
	t.Helper()
	checker, err := ParseConfig([]byte(input), "test.cue")
	qt.Assert(t, qt.IsNil(err))
	return checker
}

func checkDenied(t *testing.T, err error) *DenialError {
	t.Helper()
	var de *DenialError
	qt.Assert(t, qt.IsTrue(errors.As(err, &de)))
	return de
}

func makeRef(cueModPath, cueModVersion, goModPath, goModVersion, name string) goplugin.ResolvedReference {
	return goplugin.ResolvedReference{
		CUEModule:     module.MustNewVersion(cueModPath, cueModVersion),
		CUEImportPath: cueModPath + ":pkg",
		GoModule:      goplugin.GoModule{Path: goModPath, Version: goModVersion},
		GoImportPath:  goModPath,
		Name:          name,
	}
}
