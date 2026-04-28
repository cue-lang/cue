package cmd

import (
	"bytes"
	"fmt"
	"go/format"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"text/template"

	"github.com/spf13/cobra"
	gomodfile "golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"cuelang.org/go/cue/load"
)

func newPluginBuildGoCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build-go",
		Short: "generate Go plugin injection code",
		Long: `Build-go generates a Go source file containing a function that provides
CUE plugin injection options for use in a custom Go program that embeds the
CUE evaluator.

Unlike "cue plugin build", which produces a standalone cue binary with plugins
baked in, this command is for Go developers who want to use CUE plugins from
their own Go code.

The command uses all packages in the current module's dependency graph that
contribute @extern(go) plugins.

The generated function returns a cuecontext.Option that can be passed to
cuecontext.New to register the plugins.
`,
		RunE: mkRunE(c, runPluginBuildGo),
		Args: cobra.ExactArgs(0),
	}
	cmd.Flags().StringP("package", "p", "", "Go package name (default: Go package name of current directory)")
	cmd.Flags().StringP("func", "f", "CUEPlugins", "name of generated function")
	cmd.Flags().StringP("output", "o", "cue_plugins_gen.go", "output filename")
	return cmd
}

func runPluginBuildGo(cmd *Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	goModPath, err := findGoModPath(cwd)
	if err != nil {
		return fmt.Errorf("not in a Go module: %v", err)
	}
	goModDir := filepath.Dir(goModPath)

	// Determine the Go import path of the current directory so we can
	// avoid self-importing it in the generated code.
	localPkgPath, err := localGoPackagePath(goModPath, goModDir, cwd)
	if err != nil {
		return err
	}

	goPkgName, err := cmd.Flags().GetString("package")
	if err != nil {
		return err
	}
	if goPkgName == "" {
		goPkgName = currentGoPackageName(cwd)
	}

	funcName, err := cmd.Flags().GetString("func")
	if err != nil {
		return err
	}
	outputFile, err := cmd.Flags().GetString("output")
	if err != nil {
		return err
	}
	outputPath := filepath.Join(cwd, outputFile)

	reg, err := getRegistry()
	if err != nil {
		return err
	}
	cfg := &load.Config{
		Dir:      modRoot,
		Registry: reg,
	}
	binst := load.Instances([]string{"./..."}, cfg)
	for _, inst := range binst {
		if inst.Err != nil {
			return inst.Err
		}
	}
	if len(binst) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no plugins needed")
		return nil
	}

	refs, err := collectAllReferences(binst)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no plugins needed")
		return nil
	}

	regClient, err := newRegistryClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	resolved, goModules, err := resolveGoPackages(ctx, regClient, refs)
	if err != nil {
		return err
	}

	if err := addGoModRequirements(cmd, goModPath, goModDir, goModules); err != nil {
		return err
	}

	interimData, err := generateInterimBuildGoFile(goPkgName, resolved, localPkgPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, interimData, 0o666); err != nil {
		return err
	}
	if err := runGoCommand(cmd, goModDir, "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy: %v", err)
	}

	// Check trust before proceeding.
	manifest, err := buildManifest(refs, resolved, goModDir)
	if err != nil {
		return fmt.Errorf("building manifest: %v", err)
	}
	if err := checkPluginTrust(manifest, refs, resolved); err != nil {
		return err
	}

	imports, refEntries, err := resolveAndBuildRefs(goModDir, resolved, localPkgPath)
	if err != nil {
		return err
	}

	cuePackages := uniqueCUEPackages(refs)

	finalData, err := generateBuildGoFile(goPkgName, funcName, cuePackages, imports, refEntries)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, finalData, 0o666)
}

// localGoPackagePath returns the Go import path of the package in cwd,
// derived from the go.mod module path and the relative directory.
func localGoPackagePath(goModPath, goModDir, cwd string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %v", err)
	}
	goMod, err := gomodfile.Parse(goModPath, data, nil)
	if err != nil {
		return "", fmt.Errorf("parsing go.mod: %v", err)
	}
	modPath := goMod.Module.Mod.Path
	rel, err := filepath.Rel(goModDir, cwd)
	if err != nil {
		return "", fmt.Errorf("computing relative path: %v", err)
	}
	if rel == "." {
		return modPath, nil
	}
	return path.Join(modPath, filepath.ToSlash(rel)), nil
}

// findGoModPath searches upward from dir for a go.mod file
// and returns its path.
func findGoModPath(dir string) (string, error) {
	for d := dir; ; {
		p := filepath.Join(d, "go.mod")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod found in or above %s", dir)
		}
		d = parent
	}
}

// currentGoPackageName returns the Go package name of the current directory,
// falling back to the base directory name if no Go files are found.
func currentGoPackageName(dir string) string {
	pkgs, err := packages.Load(&packages.Config{
		Dir:  dir,
		Mode: packages.NeedName,
	}, ".")
	if err == nil && len(pkgs) > 0 && pkgs[0].Name != "" {
		return pkgs[0].Name
	}
	return filepath.Base(dir)
}

// addGoModRequirements adds Go module requirements that are not already
// present in the user's go.mod file. It skips the user's own module
// (a module never needs to require or replace itself).
func addGoModRequirements(cmd *Command, goModPath string, goModDir string, modules []goModRequire) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %v", err)
	}
	goMod, err := gomodfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("parsing go.mod: %v", err)
	}

	ownModule := goMod.Module.Mod.Path
	existing := make(map[string]bool)
	for _, req := range goMod.Require {
		existing[req.Mod.Path] = true
	}
	existingReplace := make(map[string]bool)
	for _, rep := range goMod.Replace {
		existingReplace[rep.Old.Path] = true
	}

	for _, m := range modules {
		if m.Path == ownModule {
			continue
		}
		if !existing[m.Path] {
			if err := runGoCommand(cmd, goModDir, "mod", "edit",
				fmt.Sprintf("-require=%s@%s", m.Path, m.Version)); err != nil {
				return fmt.Errorf("adding module requirement %s@%s: %v", m.Path, m.Version, err)
			}
		}
		if m.Replace != "" && !existingReplace[m.Path] {
			if err := runGoCommand(cmd, goModDir, "mod", "edit",
				fmt.Sprintf("-replace=%s=%s", m.Path, m.Replace)); err != nil {
				return fmt.Errorf("adding module replace %s=%s: %v", m.Path, m.Replace, err)
			}
		}
	}
	return nil
}

var buildGoExtraImports = []string{
	"cuelang.org/go/cue",
	"cuelang.org/go/cue/cuecontext",
	"cuelang.org/go/cue/inject/goplugin",
}

func generateInterimBuildGoFile(pkgName string, resolved []resolvedRef, localPkgPath string) ([]byte, error) {
	pkgs := make(map[string]bool)
	for _, r := range resolved {
		if r.goPkg != localPkgPath {
			pkgs[r.goPkg] = true
		}
	}
	for _, p := range buildGoExtraImports {
		pkgs[p] = true
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by cue plugin build-go; DO NOT EDIT.\n\n")
	fmt.Fprintf(&buf, "package %s\n\nimport (\n", pkgName)
	for _, pkg := range slices.Sorted(maps.Keys(pkgs)) {
		fmt.Fprintf(&buf, "\t_ %q\n", pkg)
	}
	buf.WriteString(")\n")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting interim Go file: %v", err)
	}
	return formatted, nil
}

// uniqueCUEPackages returns the sorted unique CUE import paths
// from the collected references.
func uniqueCUEPackages(refs []instanceReference) []string {
	seen := make(map[string]bool)
	for _, ir := range refs {
		seen[ir.inst.ImportPath] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

var buildGoTmpl = template.Must(template.New("build-go").Parse(`// Code generated by cue plugin build-go; DO NOT EDIT.

package {{.Package}}

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
{{range .Imports}}
	{{.Alias}} {{printf "%q" .Path}}
{{- end}}
)

// {{.FuncName}} returns a CUE context option that registers Go plugins
// provided by the following packages and their dependencies:
//
{{- range .CUEPackages}}
//	{{.}}
{{- end}}
func {{.FuncName}}() cuecontext.Option {
	refs := map[goplugin.Reference]func() cue.Value{
{{- range .Refs}}
		{
			GoPackage:        {{printf "%q" .GoPackage}},
{{- with .CUEModuleVersion}}
			CUEModuleVersion: {{printf "%q" .}},
{{- end}}
{{- with .CUEInstance}}
			CUEInstance:      {{printf "%q" .}},
{{- end}}
			Name:             {{printf "%q" .Name}},
		}: func() cue.Value {
			return {{.WrapExpr}}
		},
{{- end}}
	}
	return cuecontext.WithInjection(goplugin.Injection(refs))
}
`))

func generateBuildGoFile(pkgName, funcName string, cuePackages []string, imports []pluginImport, refs []pluginRefEntry) ([]byte, error) {
	var buf bytes.Buffer
	err := buildGoTmpl.Execute(&buf, struct {
		Package     string
		FuncName    string
		CUEPackages []string
		Imports     []pluginImport
		Refs        []pluginRefEntry
	}{
		Package:     pkgName,
		FuncName:    funcName,
		CUEPackages: cuePackages,
		Imports:     imports,
		Refs:        refs,
	})
	if err != nil {
		return nil, fmt.Errorf("generating Go file: %v", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated Go file: %v\n%s", err, buf.Bytes())
	}
	return formatted, nil
}
