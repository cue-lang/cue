// Copyright 2026 The CUE Authors
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
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/eval"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/source"
	"github.com/spf13/cobra"
)

// newDocCmd creates the doc command
func newDocCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "docs for a pkg",
		RunE:  mkRunE(c, runDoc),
	}

	addOrphanFlags(cmd)
	addInjectionFlags(cmd)

	return cmd
}

func runDoc(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{mode: filetypes.Input})
	if err != nil {
		return err
	}

	fs := fscache.NewCUECachedFS()
	now := time.Now()

	for _, inst := range b.insts {
		if len(inst.BuildFiles) == 0 {
			continue
		}
		fs := fscache.NewOverlayFS(fs)

		parserCfg := parser.NewConfig()
		modFile := inst.ModuleFile
		if modFile == nil {
			continue
		}
		if modFile.Language != nil {
			versionOption := parser.Version(modFile.Language.Version)
			parserCfg = parser.NewConfig(versionOption)
		}

		asts := make([]*ast.File, len(inst.BuildFiles))
		err := fs.Update(func(txn *fscache.UpdateTxn) error {
			for i, file := range inst.BuildFiles {
				src, err := source.ReadAll(file.Filename, file.Source)
				if err != nil {
					return err
				}
				uri := protocol.URIFromPath(file.Filename)
				fh, err := txn.Set(uri, src, now, 0)
				if err != nil {
					return err
				}
				syntax, _, err := fh.ReadCUE(parserCfg)
				if syntax == nil {
					return err
				}
				asts[i] = syntax
			}
			return nil
		})
		if err != nil {
			return err
		}

		ip := ast.ImportPath{Qualifier: asts[0].PackageName()}
		modPath, version, _ := ast.SplitPackageVersion(modFile.QualifiedModule())

		pkgPath, err := filepath.Rel(inst.Root, inst.Dir)
		ip.Path = modPath + "/" + filepath.ToSlash(pkgPath)
		ip.Version = version
		ip = ip.Canonical()
		fmt.Println("Docs for pkg", ip)
		fmt.Println()

		evalCfg := eval.Config{IP: ip}
		e := eval.New(evalCfg, asts...)
		for key, nodes := range e.Exported() {
			node := nodes[0]
			pos := node.Pos()
			fi := e.ForFile(pos.Filename())
			comments := fi.DocCommentsForOffset(pos.Offset())

			keys := slices.Collect(maps.Keys(comments))
			slices.SortFunc(keys, func(a, b ast.Node) int {
				aPos, bPos := a.Pos().Position(), b.Pos().Position()
				return cmp.Compare(aPos.Filename, bPos.Filename)
			})

			var sb strings.Builder
			sb.WriteString(key)
			sb.WriteByte('\n')
			for _, key := range keys {
				for _, cg := range comments[key] {
					text := cg.Text()
					text = strings.TrimRight(text, "\n")
					if text == "" {
						continue
					}
					fmt.Fprintln(&sb, text)
				}
			}
			fmt.Println(sb.String())
		}
	}

	return nil
}
