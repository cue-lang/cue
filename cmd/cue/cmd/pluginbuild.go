package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"go/types"
	"log"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	gomodfile "golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modregistry"
)

const (
	cueModulePath  = "cuelang.org/go"
	cuePackagePath = cueModulePath + "/cue"
)

func newPluginBuildCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "build a Go plugin binary for the current module",
		Long: `Build generates a Go wrapper main module in cue.mod/plugin/ and builds a
cached plugin binary that extends the cue command with all required Go
plugin values implied by the module's dependencies.

The generated code and its associated go.mod file are created inside the
cue.mod directory. The built binary is placed in the CUE cache directory.

Once a plugin executable is built, the regular cue binary detects it
and re-executes it to do all the work.
`,
		RunE: mkRunE(c, runPluginBuild),
		Args: cobra.ExactArgs(0),
	}
	return cmd
}

func runPluginBuild(cmd *Command, args []string) error {
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	reg, err := getCachedRegistry()
	if err != nil {
		return err
	}

	cfg := &load.Config{
		Dir:      modRoot,
		Registry: reg,
	}
	binst := load.Instances([]string{"./..."}, cfg)
	if len(binst) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no plugins needed")
		return nil
	}

	for _, inst := range binst {
		if inst.Err != nil {
			return inst.Err
		}
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

	// Phase 1: Resolve Go packages from go.mod files found
	// alongside the CUE instances.
	resolved, goModules, err := resolveGoPackages(ctx, regClient, refs)
	if err != nil {
		return err
	}

	// Phase 2: Generate interim go.mod and main.go (with blank imports),
	// then run go mod tidy to resolve all dependencies.
	pluginDir := filepath.Join(modRoot, "cue.mod", "plugin")
	if err := os.MkdirAll(pluginDir, 0o777); err != nil {
		return fmt.Errorf("creating plugin directory: %v", err)
	}

	goModData, err := generateGoMod(goModules)
	if err != nil {
		return err
	}
	interimMainData, err := generateInterimMainGo(resolved)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "go.mod"), goModData, 0o666); err != nil {
		return err
	}
	log.Printf("go.mod contents: %q", goModData)
	if err := os.WriteFile(filepath.Join(pluginDir, "main.go"), interimMainData, 0o666); err != nil {
		return err
	}
	if err := runGoCommand(cmd, pluginDir, "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy: %v", err)
	}
	// Phase 3: Load packages with go/types to inspect function signatures,
	// then generate the final main.go with typed wrappers.
	imports, refEntries, err := resolveAndBuildRefs(pluginDir, resolved)
	if err != nil {
		return err
	}

	mainGoData, err := generateMainGo(imports, refEntries)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "main.go"), mainGoData, 0o666); err != nil {
		return err
	}

	// Generate and write the plugin manifest.
	manifest, err := buildManifest(refs, resolved, pluginDir)
	if err != nil {
		return fmt.Errorf("building manifest: %v", err)
	}
	manifestData, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return fmt.Errorf("encoding manifest: %v", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifestData, 0o666); err != nil {
		return err
	}

	// Phase 4: Build the binary.
	binPath, err := pluginExePath(pluginDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(binPath), 0o777); err != nil {
		return fmt.Errorf("creating plugin binary directory: %v", err)
	}
	if err := runGoCommand(cmd, pluginDir, "build", "-trimpath", "-o", binPath, "."); err != nil {
		return fmt.Errorf("go build: %v", err)
	}
	return nil
}

type instanceReference struct {
	inst *build.Instance
	ref  goplugin.Reference
}

// collectAllReferences walks the import graph from the given instances
// and collects all goplugin.Reference values from @go attributes.
func collectAllReferences(instances []*build.Instance) ([]instanceReference, error) {
	var refs []instanceReference
	seen := make(map[string]bool)
	var walk func(inst *build.Instance) error
	walk = func(inst *build.Instance) error {
		if seen[inst.ImportPath] {
			return nil
		}
		seen[inst.ImportPath] = true
		for ref, err := range goplugin.References(inst) {
			if err != nil {
				return err
			}
			refs = append(refs, instanceReference{
				inst: inst,
				ref:  ref,
			})
		}
		for _, imp := range inst.Imports {
			if err := walk(imp); err != nil {
				return err
			}
		}
		return nil
	}
	for _, inst := range instances {
		if err := walk(inst); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// resolvedRef holds a plugin reference with its resolved full Go package path.
type resolvedRef struct {
	ref   goplugin.Reference
	goPkg string // full Go import path (resolved from "." when needed)
}

// goModRequire holds a Go module path and version for a require directive.
type goModRequire struct {
	Path    string
	Version string
	Replace string
}

type goModule struct {
	dir string
	mod *gomodfile.File
}

func (m goModule) path() string {
	return m.mod.Module.Mod.Path
}

// resolveGoPackages resolves each reference's Go package path by finding
// and parsing go.mod files alongside CUE instance directories. It returns
// the resolved references and a list of Go module requirements,
// including a module requirement on the CUE module itself.
func resolveGoPackages(ctx context.Context, regClient *modregistry.Client, refs []instanceReference) ([]resolvedRef, []goModRequire, error) {
	// Cache parsed go.mod files by the directory they were found in.
	goModCache := map[string]goModule{}

	var resolved []resolvedRef
	moduleSet := map[string]goModRequire{}

	for _, ir := range refs {
		if ir.inst.Root == "" {
			return nil, nil, fmt.Errorf("CUE instance %q is not in a module", ir.inst.ImportPath)
		}
		goMod, err := findGoMod(ir.inst.Dir, goModCache, ir.inst.Root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving Go module for CUE instance %s: %v", ir.inst.ImportPath, err)
		}
		goPkg := ir.ref.GoPackage
		if goPkg == "." {
			// Resolve local package: go.mod module path + relative directory.
			rel, err := filepath.Rel(goMod.dir, ir.inst.Dir)
			if err != nil {
				return nil, nil, fmt.Errorf("computing relative path for %s: %v", ir.inst.ImportPath, err)
			}
			goPkg = path.Join(goMod.mod.Module.Mod.Path, filepath.ToSlash(rel))
		}

		// Look up which Go module provides this package.
		req, err := requirementForPackage(ctx, regClient, goMod, goPkg, ir.inst)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot find requirement for package %q in module %s: %v", goPkg, goMod.path(), err)
		}
		if req.Path == cueModulePath {
			// TODO we could probably relax this restriction if needed, with a bit more
			// thought. For now, it's easier to prohibit it as the interaction with the
			// top level CUE requirement is tricky to understand.
			return nil, nil, fmt.Errorf("plugin reference to cuelang.org/go is not allowed")
		}
		// TODO error if the same module is mentioned twice?
		moduleSet[req.Path] = req

		resolved = append(resolved, resolvedRef{
			ref:   ir.ref,
			goPkg: goPkg,
		})
	}
	// Add a module requirement for the CUE module itself.
	// The package reference will be added explicitly later.
	cueReq, err := cueModRequirement()
	if err != nil {
		return nil, nil, err
	}
	moduleSet[cueModulePath] = cueReq

	return resolved, slices.Collect(maps.Values(moduleSet)), nil
}

// cueModRequirement returns best-effort approximation
// to a module requirement corresponding to the currently
// running cue binary.
func cueModRequirement() (goModRequire, error) {
	cueVersion := cueversion.ModuleVersion()
	if semver.IsValid(cueVersion) {
		return goModRequire{
			Path:    cueModulePath,
			Version: cueVersion,
		}, nil
	}
	cueReplace := os.Getenv("CUE_PLUGIN_CUE_REPLACE")
	if cueReplace == "" {
		return goModRequire{}, fmt.Errorf("cannot determine cuelang.org/go version (got %q); set CUE_PLUGIN_CUE_REPLACE to a local path", cueVersion)
	}
	return goModRequire{
		Path:    cueModulePath,
		Version: "v0.0.0",
		Replace: cueReplace,
	}, nil
}

// findGoMod searches upward from dir for a go.mod file and returns
// the directory containing it and the parsed file.
func findGoMod(dir string, cache map[string]goModule, cueModuleRoot string) (goModule, error) {
	for d := dir; ; {
		if m, ok := cache[d]; ok {
			return m, nil
		}
		goModPath := filepath.Join(d, "go.mod")
		data, err := os.ReadFile(goModPath)
		if err == nil {
			f, parseErr := gomodfile.Parse(goModPath, data, nil)
			if parseErr != nil {
				return goModule{}, fmt.Errorf("parsing %s: %v (%q)", goModPath, parseErr, data)
			}
			m := goModule{
				dir: d,
				mod: f,
			}
			cache[d] = m
			return m, nil
		}
		if len(d) < len(cueModuleRoot) {
			return goModule{}, fmt.Errorf("no go.mod found in or above %s", dir)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return goModule{}, fmt.Errorf("no go.mod found in or above %s", dir)
		}
		d = parent
	}
}

// goModuleForPackage finds which Go module in the given go.mod provides
// the specified package path as imported inside the given CUE instance.
//
// If the package is not found as a requirement in the Go module, we
// don't have a version for it, so we first attempt to find a version
// by using the commit from the CUE module metadata, falling back
// to using a directory replace directive if that isn't present.
func requirementForPackage(ctx context.Context, regClient *modregistry.Client, goMod goModule, pkgPath string, foundIn *build.Instance) (goModRequire, error) {
	var req goModRequire
	for _, mreq := range goMod.mod.Require {
		if hasPathPrefix(pkgPath, mreq.Mod.Path) && len(mreq.Mod.Path) > len(req.Path) {
			req = goModRequire{
				Path:    mreq.Mod.Path,
				Version: mreq.Mod.Version,
			}
		}
	}
	if p := goMod.mod.Module.Mod.Path; req.Path == "" && hasPathPrefix(pkgPath, p) {
		if v, ok := vcsVersion(ctx, regClient, foundIn); ok {
			req = goModRequire{
				Path:    goMod.path(),
				Version: v,
			}
		} else {
			req = goModRequire{
				Path:    goMod.path(),
				Version: "v0.0.0",
				Replace: goMod.dir,
			}
		}
	}
	if req.Path == "" {
		return goModRequire{}, fmt.Errorf("package not present in module file requirements")
	}
	// Error if the module is replaced.
	// TODO maybe we should just ignore the replacement?
	for _, repl := range goMod.mod.Replace {
		if repl.Old.Path == req.Path {
			return goModRequire{}, fmt.Errorf("package is replaced")
		}
	}
	return req, nil
}

// uniquePackages returns the sorted unique Go package paths from the resolved references.
func uniquePackages(resolved []resolvedRef) []string {
	pkgs := map[string]bool{}
	for _, r := range resolved {
		pkgs[r.goPkg] = true
	}
	return slices.Sorted(maps.Keys(pkgs))
}

// pluginImport holds the import alias and path for a Go package.
type pluginImport struct {
	Alias string
	Path  string
}

// pluginRefEntry holds a reference and the expression to wrap it.
type pluginRefEntry struct {
	goplugin.Reference
	WrapExpr string
}

// resolveAndBuildRefs loads the Go packages with type information and builds
// the import list and reference entries needed for the final generated main.go.
func resolveAndBuildRefs(pluginDir string, resolved []resolvedRef) ([]pluginImport, []pluginRefEntry, error) {
	pkgPaths := uniquePackages(resolved)

	loadCfg := &packages.Config{
		Dir:  pluginDir,
		Mode: packages.NeedName | packages.NeedTypes,
	}
	pkgs, err := packages.Load(loadCfg, pkgPaths...)
	if err != nil {
		return nil, nil, fmt.Errorf("loading Go packages: %v", err)
	}

	pkgMap := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, nil, fmt.Errorf("errors loading package %s: %v", pkg.PkgPath, pkg.Errors[0])
		}
		pkgMap[pkg.PkgPath] = pkg
	}

	// Build import aliases using actual Go package names from the loaded packages.
	aliasForPkg := map[string]string{}
	aliasCount := map[string]int{}

	for _, pkgPath := range pkgPaths {
		pkg := pkgMap[pkgPath]
		if pkg == nil {
			return nil, nil, fmt.Errorf("package %s not found in loaded packages", pkgPath)
		}
		name := pkg.Name
		count := aliasCount[name]
		aliasCount[name]++
		alias := name
		if count > 0 {
			alias = fmt.Sprintf("%s%d", name, count+1)
		}
		aliasForPkg[pkgPath] = alias
	}

	var imports []pluginImport
	for _, pkgPath := range pkgPaths {
		imports = append(imports, pluginImport{
			Alias: aliasForPkg[pkgPath],
			Path:  pkgPath,
		})
	}

	// Build a qualifier for types.TypeString that uses our import aliases.
	qualifier := func(p *types.Package) string {
		if alias, ok := aliasForPkg[p.Path()]; ok {
			return alias
		}
		return p.Name()
	}

	var refEntries []pluginRefEntry
	for _, r := range resolved {
		pkg := pkgMap[r.goPkg]
		if pkg == nil {
			return nil, nil, fmt.Errorf("package %s not loaded", r.goPkg)
		}

		obj := pkg.Types.Scope().Lookup(r.ref.Name)
		if obj == nil {
			return nil, nil, fmt.Errorf("function %s not found in package %s", r.ref.Name, r.goPkg)
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return nil, nil, fmt.Errorf("%s.%s is not a function", r.goPkg, r.ref.Name)
		}

		sig := fn.Type().(*types.Signature)
		alias := aliasForPkg[r.goPkg]

		wrapExpr, err := buildWrapExpr(alias, r.ref.Name, sig, qualifier)
		if err != nil {
			return nil, nil, fmt.Errorf("building wrapper for %s.%s: %v", r.goPkg, r.ref.Name, err)
		}

		refEntries = append(refEntries, pluginRefEntry{
			Reference: r.ref,
			WrapExpr:  wrapExpr,
		})
	}

	return imports, refEntries, nil
}

// buildWrapExpr generates the Go expression to wrap a function using
// the appropriate cue.NewPureFuncN variant.
func buildWrapExpr(alias, funcName string, sig *types.Signature, qualifier types.Qualifier) (string, error) {
	params := sig.Params()
	results := sig.Results()

	nParams := params.Len()
	if nParams < 1 || nParams > 10 {
		return "", fmt.Errorf("function has %d parameters (need 1-10)", nParams)
	}

	hasError := false
	switch results.Len() {
	case 1:
		// Single return value, no error.
	case 2:
		if results.At(1).Type().String() == "error" {
			hasError = true
		} else {
			return "", fmt.Errorf("second return value must be error, got %s", results.At(1).Type())
		}
	default:
		return "", fmt.Errorf("function must return 1 or 2 values, got %d", results.Len())
	}

	qualifiedFunc := alias + "." + funcName

	if hasError {
		return fmt.Sprintf("cue.NewPureFunc%d(%s)", nParams, qualifiedFunc), nil
	}

	// Generate a wrapper lambda that adds the nil error return.
	var paramDecls, paramNames []string
	for i := range nParams {
		name := fmt.Sprintf("a%d", i)
		typeStr := types.TypeString(params.At(i).Type(), qualifier)
		paramDecls = append(paramDecls, name+" "+typeStr)
		paramNames = append(paramNames, name)
	}

	resultTypeStr := types.TypeString(results.At(0).Type(), qualifier)

	return fmt.Sprintf("cue.NewPureFunc%d(func(%s) (%s, error) {\n\t\t\treturn %s(%s), nil\n\t\t})",
		nParams,
		strings.Join(paramDecls, ", "),
		resultTypeStr,
		qualifiedFunc,
		strings.Join(paramNames, ", ")), nil
}

// goVersionForMod returns a Go version string suitable for a go.mod file.
func goVersionForMod() string {
	// runtime.Version() returns something like "go1.23.4".
	v := runtime.Version()
	// When Go is a development version, it can
	// include a date and time too, and a hyphenated
	// pre-release version, so strip that.
	// (note that the prerelease version doesn't fit with
	// the standard semver syntax so we cannot use
	// primitives from the semver package here.
	v, _, _ = strings.Cut(v, " ")
	v, _, _ = strings.Cut(v, "-")
	v, _ = strings.CutPrefix(v, "go")
	// go.mod only wants major.minor, not patch.
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

func generateGoMod(modules []goModRequire) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "module cue_plugin_gen\n\n")
	fmt.Fprintf(&buf, "go %s\n", goVersionForMod())

	replacements := make(map[string]string)
	if len(modules) > 0 {
		fmt.Fprintf(&buf, "\nrequire (\n")
		for _, m := range modules {
			fmt.Fprintf(&buf, "\t%s %s\n", m.Path, m.Version)
			if m.Replace != "" {
				replacements[m.Path] = m.Replace
			}
		}
		fmt.Fprintf(&buf, ")\n")
	}
	if len(replacements) > 0 {
		fmt.Fprintf(&buf, "\nreplace (\n")
		for _, m := range slices.Sorted(maps.Keys(replacements)) {
			fmt.Fprintf(&buf, "\t%s => %q\n", m, replacements[m])
		}
		fmt.Fprintf(&buf, ")\n")
	}
	return buf.Bytes(), nil
}

var extraImports = []string{
	"cuelang.org/go/cmd/cue/cmd",
	"cuelang.org/go/cue",
	"cuelang.org/go/cue/cuecontext",
	"cuelang.org/go/cue/inject/goplugin",
}

// generateInterimMainGo generates a minimal main.go with blank imports
// for all referenced Go packages plus the cuelang.org/go packages that
// the final generated main.go will need. This ensures that a single
// go mod tidy pass resolves all dependencies.
func generateInterimMainGo(resolved []resolvedRef) ([]byte, error) {
	pkgs := map[string]bool{
		cuePackagePath: true,
	}
	for _, r := range resolved {
		pkgs[r.goPkg] = true
	}
	for _, p := range extraImports {
		pkgs[p] = true
	}
	var buf bytes.Buffer
	buf.WriteString("package main\n\nimport (\n")
	for _, pkg := range slices.Sorted(maps.Keys(pkgs)) {
		fmt.Fprintf(&buf, "\t_ %q\n", pkg)
	}
	buf.WriteString(")\n\nfunc main() {}\n")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting interim main.go: %v", err)
	}
	return formatted, nil
}

var mainGoTmpl = template.Must(template.New("main.go").Parse(`// Code generated by cue plugin build; DO NOT EDIT.

package main

import (
	"os"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
{{range .Imports}}
	{{.Alias}} {{printf "%q" .Path}}
{{- end}}
)

func main() {
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
	os.Exit(cmd.MainWithOptions(cuecontext.WithInjection(goplugin.Injection(refs))))
}
`))

func generateMainGo(imports []pluginImport, refs []pluginRefEntry) ([]byte, error) {
	var buf bytes.Buffer
	err := mainGoTmpl.Execute(&buf, struct {
		Imports []pluginImport
		Refs    []pluginRefEntry
	}{
		Imports: imports,
		Refs:    refs,
	})
	if err != nil {
		return nil, fmt.Errorf("generating main.go: %v", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated main.go: %v\n%s", err, buf.Bytes())
	}
	return formatted, nil
}

func runGoCommand(cmd *Command, dir string, goArgs ...string) error {
	goCmd := exec.Command("go", goArgs...)
	goCmd.Dir = dir
	goCmd.Env = os.Environ()
	var stderr bytes.Buffer
	goCmd.Stderr = &stderr
	goCmd.Stdout = cmd.OutOrStdout()
	if err := goCmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%v\n%s", err, stderr.String())
		}
		return err
	}
	return nil
}

// vcsVersion attempts to find a Go module version for a Go module
// by looking up the VCS commit metadata from the CUE module registry.
// It returns the commit hash and true if successful, or ("", false) if
// no version could be determined. The raw commit hash is returned
// directly; go mod tidy will resolve it to a proper pseudo-version.
func vcsVersion(ctx context.Context, regClient *modregistry.Client, foundIn *build.Instance) (string, bool) {
	mv := foundIn.ModuleVersion
	if mv.Version() == "" {
		return "", false
	}
	// TODO this is probably doing a network round-trip
	// every time, which isn't great given that we'll be
	// doing it for every package inside the module.
	// Some caching at least would be good.
	m, err := regClient.GetModule(ctx, mv)
	if err != nil {
		return "", false
	}
	meta, err := m.Metadata()
	if err != nil || meta == nil || meta.VCSCommit == "" {
		return "", false
	}
	return meta.VCSCommit, true
}

func newRegistryClient() (*modregistry.Client, error) {
	resolver, err := modconfig.NewResolver(newModConfig(""))
	if err != nil {
		return nil, err
	}
	return modregistry.NewClientWithResolver(resolver), nil
}

// execPlugin finds and executes the plugin binary for the given
// needPluginError. It returns the process exit code.
func execPlugin(npe *needPluginError) int {
	binPath, err := pluginExePath(npe.pluginDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if _, err := os.Stat(binPath); err != nil {
		fmt.Fprintf(os.Stderr, "module uses plugins; build with `cue plugin build`\n")
		return 1
	}
	cmd := exec.Command(binPath, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	return 0
}

func hasPathPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix) && (len(s) == len(prefix) || s[len(prefix)] == '/')
}

// needPluginError is returned when the current module uses plugins
// but the cue command was not invoked via MainWithOptions.
// It carries the plugin directory path so the caller can locate
// the plugin executable without repeating the lookup.
type needPluginError struct {
	pluginDir string
}

func (e *needPluginError) Error() string {
	return "module uses plugins"
}

// checkPlugin checks whether the current module uses plugins
// and the cue command was not invoked via MainWithOptions.
// It returns a *needPluginError if so, nil otherwise.
func checkPlugin(cmd *Command) error {
	if cmd.runningInPlugin {
		return nil
	}
	modRoot, err := findModuleRoot()
	if err != nil {
		return nil
	}
	pluginDir := filepath.Join(modRoot, "cue.mod", "plugin")
	if _, err := os.Stat(pluginDir); err != nil {
		return nil
	}
	return &needPluginError{pluginDir: pluginDir}
}

// pluginExePath returns the path to the plugin executable
// based on a hash of the plugin directory contents.
func pluginExePath(pluginDir string) (string, error) {
	hash, err := pluginHash(pluginDir)
	if err != nil {
		return "", err
	}
	cacheDir, err := cueconfig.CacheDir(os.Getenv)
	if err != nil {
		return "", err
	}
	binName := "cue_" + hash
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	return filepath.Join(cacheDir, "plugin", binName), nil
}

// pluginHash computes a hex-encoded SHA-256 hash of the plugin's
// go.mod and main.go files together with GOARCH and GOOS.
func pluginHash(pluginDir string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "GOARCH=%s\nGOOS=%s\n", runtime.GOARCH, runtime.GOOS)
	goModData, err := os.ReadFile(filepath.Join(pluginDir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading plugin go.mod: %v", err)
	}
	h.Write(goModData)
	mainGoData, err := os.ReadFile(filepath.Join(pluginDir, "main.go"))
	if err != nil {
		return "", fmt.Errorf("reading plugin main.go: %v", err)
	}
	h.Write(mainGoData)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// buildManifest creates a [goplugin.Manifest] from the collected references
// and the tidied go.mod in the plugin directory.
func buildManifest(refs []instanceReference, resolved []resolvedRef, pluginDir string) (*goplugin.Manifest, error) {
	goModData, err := os.ReadFile(filepath.Join(pluginDir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("reading plugin go.mod: %v", err)
	}
	goModFile, err := gomodfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing plugin go.mod: %v", err)
	}

	manifest := &goplugin.Manifest{
		Modules:    map[string]goplugin.Module{},
		GoPackages: map[string]goplugin.GoModule{},
	}

	for i, ir := range refs {
		r := resolved[i]
		modPath := ir.inst.ModuleVersion.Path()
		cueImportPath := ir.inst.ImportPath

		mod := manifest.Modules[modPath]
		if mod.Instances == nil {
			mod.Instances = map[string]goplugin.Instance{}
		}
		inst := mod.Instances[cueImportPath]
		if inst.GoPackages == nil {
			inst.GoPackages = map[string][]string{}
		}

		goPkg := r.goPkg
		names := inst.GoPackages[goPkg]
		if !slices.Contains(names, ir.ref.Name) {
			inst.GoPackages[goPkg] = append(names, ir.ref.Name)
		}

		if ir.ref.GoPackage == "." {
			inst.ColocatedGoPackage = goPkg
		}

		mod.Instances[cueImportPath] = inst
		manifest.Modules[modPath] = mod

		if _, ok := manifest.GoPackages[goPkg]; !ok {
			manifest.GoPackages[goPkg] = manifestGoModule(goModFile, goPkg)
		}
	}

	return manifest, nil
}

// manifestGoModule finds the Go module that provides the given package
// by matching against the requirements in the given go.mod file.
func manifestGoModule(goModFile *gomodfile.File, pkgPath string) goplugin.GoModule {
	var bestPath string
	var bestVersion string
	for _, req := range goModFile.Require {
		if hasPathPrefix(pkgPath, req.Mod.Path) && len(req.Mod.Path) > len(bestPath) {
			bestPath = req.Mod.Path
			bestVersion = req.Mod.Version
		}
	}
	if bestPath == "" {
		return goplugin.GoModule{}
	}
	for _, rep := range goModFile.Replace {
		if rep.Old.Path == bestPath && rep.New.Version == "" {
			return goplugin.GoModule{
				Path: bestPath,
				Dir:  rep.New.Path,
			}
		}
	}
	return goplugin.GoModule{
		Path:    bestPath,
		Version: bestVersion,
	}
}

// pluginCheckerOption reads the manifest file from cue.mod/plugin/manifest.json
// and returns a cuecontext.Option that adds a Checker injection.
// It returns ok as false when no manifest file is found.
// It returns an error if the manifest file exists but cannot be parsed.
func appendPluginCheckerOptions(cmd *Command, opts []cuecontext.Option) (opt []cuecontext.Option, err error) {
	if !cmd.runningInPlugin {
		// Not running in plugin: no need for checking plugins.
		return opts, nil
	}
	modRoot, err := findModuleRoot()
	if err != nil {
		return opts, nil
	}
	data, err := os.ReadFile(filepath.Join(modRoot, "cue.mod", "plugin", "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return opts, nil
		}
		return nil, err
	}
	var manifest goplugin.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("cannot read plugin manifest: %v", err)
	}
	return append(opts,
		cuecontext.WithInjection(
			goplugin.Checker(&manifest, func(goplugin.ResolvedReference) error {
				return nil
			}),
		),
	), nil
}
