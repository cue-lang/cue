// Copyright 2025 The CUE Authors
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
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/encoding"
)

func newCRDCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crd <files>",
		Short: "convert Kubernetes CRDs to packages in the current module",
		Long: `crd converts Kubernetes Custom Resource Definitions (CRDs)
to CUE packages.

It reads all the argument files and creates a version package
in the current directory for each CRD found that matches the group
specified by the --group flag, as determined by the spec.group field.

If the --group flag is not provided, then all the CRDs found must have the same group.

Each package contains a definition named after the spec.names.kind field in
each extracted CRD.

Example:

	cue get crd --group example.com ./crds/*.yaml
	curl https://raw.githubusercontent.com/example/crd.yaml | cue get crd yaml: -
`,
		RunE: mkRunE(c, runCRD),
	}

	cmd.Flags().StringP(string(flagGroup), "g", "", "CRD group to filter by")
	return cmd
}

func runCRD(cmd *Command, args []string) error {
	group := flagGroup.String(cmd)
	if len(args) == 0 {
		return fmt.Errorf("must specify at least one file")
	}
	// TODO could potentially support URLs as arguments, as
	// it's common to use URLs to specify CRDs in other
	// Kubernetes-related tools.
	insts := load.Instances(args, nil)
	if len(insts) != 1 {
		if len(insts) > 1 {
			return fmt.Errorf("cannot specify multiple packages to cue get crd")
		}
		// TODO although other similar places in cmd/cue check
		// for this case (load.Instances returning zero instances),
		// I believe it's not actually possible.
		return fmt.Errorf("no files or packages specified")
	}
	inst := insts[0]
	if inst.Err != nil {
		return inst.Err
	}
	if !inst.User {
		// TODO remove this restriction? It might potentially be useful
		// to import CRD data from a CUE package.
		return fmt.Errorf("input must be individual files not packages")
	}
	groups := make(map[string]bool)
	autoGroup := group == ""
	for _, f := range inst.OrphanedFiles {
		d := encoding.NewDecoder(cmd.ctx, f, nil)
		for ; !d.Done(); d.Next() {
			v := cmd.ctx.BuildFile(d.File())
			if err := v.Err(); err != nil {
				return err
			}
			crds, err := jsonschema.ExtractCRDs(v, nil)
			if err != nil {
				// TODO include the filename of the original in the error message?
				return err
			}
			for _, crd := range crds {
				if autoGroup {
					groups[crd.Data.Spec.Group] = true
					if group == "" {
						group = crd.Data.Spec.Group
					} else if crd.Data.Spec.Group != group {
						// We'll generate the error later when we know all the groups involved.
						continue
					}
				} else if crd.Data.Spec.Group != group {
					continue
				}
				for version, file := range crd.Versions {
					// TODO there's a potential security issue if the version or kind
					// contain a path separator.
					newf := &ast.File{
						Decls: []ast.Decl{
							&ast.Package{
								Name: ast.NewIdent(version),
							},
							&ast.Field{
								Label: ast.NewIdent("#" + crd.Data.Spec.Names.Kind),
								Value: internal.ToExpr(file),
							},
						},
					}
					// Add appropriate imports.
					if err := astutil.Sanitize(newf); err != nil {
						return err
					}
					data, err := format.Node(newf)
					if err != nil {
						return err
					}
					if err := os.MkdirAll(version, 0o777); err != nil {
						return err
					}
					log.Printf("writing %s", filepath.Join(version, crd.Data.Spec.Names.Singular+".cue"))
					if err := os.WriteFile(filepath.Join(version, crd.Data.Spec.Names.Singular+".cue"), data, 0o666); err != nil {
						return err
					}
				}
			}
		}
		if err := d.Err(); err != nil {
			return err
		}
	}
	if autoGroup && len(groups) > 1 {
		return fmt.Errorf("multiple CRD groups found: %v", strings.Join(slices.Sorted(maps.Keys(groups)), " "))
	}
	return nil
}
