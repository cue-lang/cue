package load

import (
	_ "embed"

	"encoding/json"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/runtime"
)

//go:embed moduleschema.cue
var moduleSchema []byte

type modFile struct {
	Module     string `json:"module"`
	CUE        string `json:"cue"`
	Deprecated string `json:"deprecated"`
	Deps       map[string]*modDep
	Retract    []*modRetractedVersion
}

type modDep struct {
	Version    string                     `json:"v"`
	Exclude    map[string]bool            `json:"exclude"`
	Replace    map[string]*modReplacement `json:"replace"`
	ReplaceAll *modReplacement            `json:"replaceAll"`
}

type modRetractedVersion struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type modReplacement struct {
	LocalPath string `json:"-"`
	Module    string `json:"m"`
	Version   string `json:"v"`
}

func (repl *modReplacement) UnmarshalJSON(data []byte) error {
	if data[0] == '{' {
		type modReplacementNoMethods modReplacement
		return json.Unmarshal(data, (*modReplacementNoMethods)(repl))
	}
	return json.Unmarshal(data, &repl.LocalPath)
}

// loadModule loads the module file, resolves and downloads module
// dependencies. It sets c.Module if it's empty or checks it for
// consistency with the module file otherwise.
func (c *Config) loadModule() error {
	// TODO: also make this work if run from outside the module?
	mod := filepath.Join(c.ModuleRoot, modDir)
	info, cerr := c.fileSystem.stat(mod)
	if cerr != nil {
		return nil
	}
	// TODO remove support for legacy non-directory module.cue file
	// by returning an error if info.IsDir is false.
	if info.IsDir() {
		mod = filepath.Join(mod, moduleFile)
	}
	f, cerr := c.fileSystem.openFile(mod)
	if cerr != nil {
		return nil
	}
	defer f.Close()

	// TODO: move to full build again
	file, err := parser.ParseFile("load", f)
	if err != nil {
		return errors.Wrapf(err, token.NoPos, "invalid cue.mod file")
	}
	// TODO disallow non-data-mode CUE.

	ctx := (*cue.Context)(runtime.New())
	schemav := ctx.CompileBytes(moduleSchema, cue.Filename("$cueroot/cue/load/moduleschema.cue"))
	if err := schemav.Validate(); err != nil {
		return errors.Wrapf(err, token.NoPos, "internal error: invalid CUE module.cue schema")
	}
	v := ctx.BuildFile(file)
	if err := v.Validate(); err != nil {
		return errors.Wrapf(err, token.NoPos, "invalid cue.mod file")
	}
	v = v.Unify(schemav)
	if err := v.Validate(); err != nil {
		return errors.Wrapf(err, token.NoPos, "invalid cue.mod file")
	}
	var mf modFile
	if err := v.Decode(&mf); err != nil {
		return errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct")
	}
	if c.Module == "" {
		c.Module = mf.Module
		return nil
	}
	if c.Module == mf.Module {
		return nil
	}
	return errors.Newf(token.NoPos, "inconsistent modules: got %q, want %q", mf.Module, c.Module)
}
