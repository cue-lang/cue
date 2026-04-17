package cmd

import (
	"bytes"
	"cmp"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

func newPluginInitGoCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-go",
		Short: "scaffold a CUE file with @extern(go) declarations from Go source",
		Long: `Init-go inspects the Go package in the current directory and generates
an initial CUE file with @extern(go) declarations for all eligible
exported functions.

A function is eligible if it:
  - is exported (starts with an uppercase letter)
  - has 1-10 parameters, all CUE-representable
  - returns 1 value or (value, error)

The current directory must be within a CUE module and must contain
a Go package.
`,
		RunE: mkRunE(c, runPluginInitGo),
		Args: cobra.ExactArgs(0),
	}
	cmd.Flags().StringP("output", "o", "", "output filename (default: <pkgname>.cue)")
	cmd.Flags().StringP("package", "p", "", "CUE package name (default: Go package name)")
	cmd.Flags().Bool("force", false, "overwrite existing file")
	return cmd
}

func runPluginInitGo(cmd *Command, args []string) error {
	if _, err := findModuleRoot(); err != nil {
		return fmt.Errorf("not in a CUE module: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	loadCfg := &packages.Config{
		Dir:  cwd,
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax,
	}
	pkgs, err := packages.Load(loadCfg, ".")
	if err != nil {
		return fmt.Errorf("loading Go package: %v", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no Go package found in current directory")
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return fmt.Errorf("no Go package found in current directory: %v", pkg.Errors[0])
	}
	if pkg.Types == nil {
		return fmt.Errorf("no Go package found in current directory")
	}

	cuePkgName, err := cmd.Flags().GetString("package")
	if err != nil {
		return err
	}
	if cuePkgName == "" {
		cuePkgName = pkg.Name
	}

	funcs := eligibleFuncs(pkg.Types.Scope())

	var buf bytes.Buffer
	buf.WriteString("@extern(go)\n\n")
	fmt.Fprintf(&buf, "package %s\n", cuePkgName)

	if len(funcs) == 0 {
		buf.WriteString("\n// No eligible exported functions were found in this package.\n")
		buf.WriteString("// Add @go() declarations here manually. For example:\n")
		buf.WriteString("//   MyFunc: _ @go()\n")
	} else {
		buf.WriteString("\n")
		for _, fn := range funcs {
			fmt.Fprintf(&buf, "%s: _ @go()\n", fn.Name())
		}
	}

	outputFile, err := cmd.Flags().GetString("output")
	if err != nil {
		return err
	}
	if outputFile == "" {
		outputFile = cuePkgName + ".cue"
	}
	outputPath := filepath.Join(cwd, outputFile)

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("file %s already exists (use --force to overwrite)", outputFile)
		}
	}

	if err := os.WriteFile(outputPath, buf.Bytes(), 0o666); err != nil {
		return err
	}
	return nil
}

// eligibleFuncs returns all exported functions in the scope that are
// suitable for use as CUE plugin functions, sorted by name.
func eligibleFuncs(scope *types.Scope) []*types.Func {
	var funcs []*types.Func
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.Recv() != nil {
			continue
		}
		if !isEligibleSignature(sig) {
			continue
		}
		funcs = append(funcs, fn)
	}
	slices.SortFunc(funcs, func(a, b *types.Func) int {
		return cmp.Compare(a.Name(), b.Name())
	})
	return funcs
}

func isEligibleSignature(sig *types.Signature) bool {
	params := sig.Params()
	results := sig.Results()
	if params.Len() < 1 || params.Len() > 10 {
		return false
	}
	switch results.Len() {
	case 1:
		// ok
	case 2:
		if results.At(1).Type().String() != "error" {
			return false
		}
	default:
		return false
	}
	for i := range params.Len() {
		if !isCUERepresentable(params.At(i).Type()) {
			return false
		}
	}
	if !isCUERepresentable(results.At(0).Type()) {
		return false
	}
	return true
}

func isCUERepresentable(t types.Type) bool {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool, types.String,
			types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
			types.Uintptr,
			types.Float32, types.Float64:
			return true
		}
	case *types.Slice:
		return isCUERepresentable(t.Elem())
	case *types.Map:
		if basic, ok := types.Unalias(t.Key()).(*types.Basic); ok && basic.Kind() == types.String {
			return isCUERepresentable(t.Elem())
		}
	case *types.Pointer:
		return isCUERepresentable(t.Elem())
	case *types.Struct:
		for i := range t.NumFields() {
			f := t.Field(i)
			if f.Exported() && !isCUERepresentable(f.Type()) {
				return false
			}
		}
		return true
	}
	return false
}
