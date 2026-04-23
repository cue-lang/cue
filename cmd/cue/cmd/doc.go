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
	"path"
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
		ip.Path = path.Clean(modPath + "/" + filepath.ToSlash(pkgPath))
		ip.Version = version
		ip = ip.Canonical()

		var sb strings.Builder
		fmt.Fprintf(&sb, "Docs for pkg %v\n", ip)

		evalCfg := eval.Config{IP: ip}
		e := eval.New(evalCfg, asts...)
		root := e.Root()
		for key, node := range root.Children() {
			writeDocs(&sb, true, "  ", 0, key, node)
		}

		fmt.Println(sb.String())
	}

	return nil
}

func writeDocs(sb *strings.Builder, recurse bool, prefix string, depth int, key string, node *eval.Node) {
	prefix0 := strings.Repeat(prefix, depth)
	prefix1 := strings.Repeat(prefix, depth+1)
	sb.WriteString(prefix0)
	sb.WriteString(key)
	sb.WriteString(":\n")

	comments := node.DocComments()

	keys := slices.Collect(maps.Keys(comments))
	slices.SortFunc(keys, func(a, b ast.Node) int {
		aPos, bPos := a.Pos().Position(), b.Pos().Position()
		switch c := cmp.Compare(aPos.Filename, bPos.Filename); c {
		case 0:
			return cmp.Compare(aPos.Offset, bPos.Offset)
		default:
			return c
		}
	})

	for _, key := range keys {
		for _, cg := range comments[key] {
			text := cg.Text()
			text = strings.TrimRight(text, "\n")
			if text == "" {
				continue
			}
			for line := range strings.Lines(text) {
				sb.WriteString(prefix1)
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	if !recurse {
		return
	}

	for key, node := range node.Children() {
		writeDocs(sb, false, prefix, depth+1, key, node)
	}
}
