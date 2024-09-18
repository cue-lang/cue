// Copyright 2024 The CUE Authors
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
	"bytes"
	"fmt"
	"os"
	"strconv"

	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newModEditCmd(c *Command) *cobra.Command {
	editCmd := &modEditCmd{}
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "edit cue.mod/module.cue",
		Long: `Edit provides a command-line interface for editing cue.mod/module.cue.
It reads only that file; it does not look up information about the modules
involved.

The editing flags specify a sequence of editing operations.

The -require=path@version and -drop-require=path@majorversion flags add
and drop a requirement on the given module path and version. Note that
-require overrides any existing requirements on path. These flags are
mainly for tools that understand the module graph. Users should prefer
'cue mod get path@version' which makes other cue.mod adjustments as
needed to satisfy constraints imposed by other modules.

The --module flag changes the module's path (the module.cue file's module field).
The --source flag changes the module's declared source.
The --drop-source flag removes the source field.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, editCmd.run),
		Args: cobra.ExactArgs(0),
	}
	addFlagVar(cmd, flagFunc(editCmd.flagSource), string(flagSource), "set the source field")
	addFlagVar(cmd, boolFlagFunc(editCmd.flagDropSource), "drop-source", "remove the source field")
	addFlagVar(cmd, flagFunc(editCmd.flagLanguageVersion), string(flagLanguageVersion), "set language.version ('current' means current language version)")
	addFlagVar(cmd, flagFunc(editCmd.flagModule), "module", "set the module path")
	addFlagVar(cmd, flagFunc(editCmd.flagRequire), "require", "add a required module@version")
	addFlagVar(cmd, flagFunc(editCmd.flagDropRequire), "drop-require", "remove a requirement")

	return cmd
}

type modEditCmd struct {
	edits []func(*modfile.File) error
}

func (c *modEditCmd) run(cmd *Command, args []string) error {
	modPath, mf, data, err := readModuleFile()
	if err != nil {
		return err
	}
	for _, edit := range c.edits {
		if err := edit(mf); err != nil {
			return err
		}
	}
	newData, err := mf.Format()
	if err != nil {
		return fmt.Errorf("invalid resulting module.cue file after edits: %v", err)
	}
	if bytes.Equal(newData, data) {
		return nil
	}
	if err := os.WriteFile(modPath, newData, 0o666); err != nil {
		return err
	}
	return nil
}

func (c *modEditCmd) addEdit(f func(*modfile.File) error) {
	c.edits = append(c.edits, f)
}

func (c *modEditCmd) flagSource(arg string) error {
	src := &modfile.Source{
		Kind: arg,
	}
	if err := src.Validate(); err != nil {
		return err
	}
	c.addEdit(func(f *modfile.File) error {
		f.Source = src
		return nil
	})
	return nil
}

func (c *modEditCmd) flagDropSource(arg bool) error {
	if !arg {
		return fmt.Errorf("cannot set --drop-source to false")
	}
	c.addEdit(func(f *modfile.File) error {
		f.Source = nil
		return nil
	})
	return nil
}

func (c *modEditCmd) flagLanguageVersion(arg string) error {
	editFunc, err := addLanguageVersion(arg)
	if err != nil {
		return err
	}
	c.addEdit(editFunc)
	return nil
}

func addLanguageVersion(v string) (func(*modfile.File) error, error) {
	if v == "current" {
		v = cueversion.LanguageVersion()
	} else {
		if semver.Canonical(v) != v {
			return nil, fmt.Errorf("language version %q is not canonical (must include major, minor and patch versions)", v)
		}

		if min := modfile.EarliestClosedSchemaVersion(); semver.Compare(v, min) < 0 {
			// TODO(rogpeppe) We might want to relax this to allow people to
			// declare an earlier language version (see https://cuelang.org/issue/3145).
			return nil, fmt.Errorf("language version %q is too early for module.cue schema (earliest allowed is %s)", v, min)
		}
		if max := cueversion.LanguageVersion(); semver.Compare(v, max) > 0 {
			return nil, fmt.Errorf("language version %q may not be after current language version %s", v, max)
		}
	}
	return func(f *modfile.File) error {
		if f.Language == nil {
			f.Language = &modfile.Language{}
		}
		f.Language.Version = v
		return nil
	}, nil
}

func (c *modEditCmd) flagModule(arg string) error {
	if err := module.CheckPath(arg); err != nil {
		return err
	}
	c.addEdit(func(f *modfile.File) error {
		f.Module = arg
		return nil
	})
	return nil
}

func (c *modEditCmd) flagRequire(arg string) error {
	v, err := module.ParseVersion(arg)
	if err != nil {
		return err
	}
	c.addEdit(func(f *modfile.File) error {
		if f.Deps == nil {
			f.Deps = make(map[string]*modfile.Dep)
		}
		vm := v.Path()
		dep := f.Deps[vm]
		if dep == nil {
			dep = &modfile.Dep{}
			f.Deps[vm] = dep
		}
		dep.Version = v.Version()
		return nil
	})
	return nil
}

func (c *modEditCmd) flagDropRequire(arg string) error {
	if err := module.CheckPath(arg); err != nil {
		return err
	}
	// TODO allow dropping a requirement without specifying
	// the major version - we can use the default field to disambiguate.
	c.addEdit(func(f *modfile.File) error {
		delete(f.Deps, arg)
		return nil
	})
	return nil
}

func addFlagVar(cmd *cobra.Command, v pflag.Value, name string, usage string) {
	flags := cmd.Flags()
	flags.Var(v, name, usage)
	// This works around https://github.com/spf13/pflag/issues/281
	if v, ok := v.(interface{ IsBoolFlag() bool }); ok && v.IsBoolFlag() {
		flags.Lookup(name).NoOptDefVal = "true"
	}
}

type flagFunc func(string) error

func (f flagFunc) String() string     { return "" }
func (f flagFunc) Set(s string) error { return f(s) }
func (f flagFunc) Type() string {
	return "string"
}

type boolFlagFunc func(bool) error

func (f boolFlagFunc) String() string { return "" }
func (f boolFlagFunc) Set(s string) error {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	return f(b)
}
func (f boolFlagFunc) Type() string {
	return "bool"
}
func (f boolFlagFunc) IsBoolFlag() bool { return true }
