// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package completion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"unicode"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/golang"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/safetoken"
	"cuelang.org/go/internal/golangorgx/tools/fuzzy"
)

// packageClauseCompletions offers completions for a package declaration when
// one is not present in the given file.
func packageClauseCompletions(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle, position protocol.Position) ([]CompletionItem, *Selection, error) {
	// We know that the AST for this file will be empty due to the missing
	// package declaration, but parse it anyway to get a mapper.
	// TODO(adonovan): opt: there's no need to parse just to get a mapper.
	pgf, err := snapshot.ParseGo(ctx, fh, golang.ParseFull)
	if err != nil {
		return nil, nil, err
	}

	offset, err := pgf.Mapper.PositionOffset(position)
	if err != nil {
		return nil, nil, err
	}
	surrounding, err := packageCompletionSurrounding(pgf, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid position for package completion: %w", err)
	}

	packageSuggestions, err := packageSuggestions(ctx, snapshot, fh.URI(), "")
	if err != nil {
		return nil, nil, err
	}

	var items []CompletionItem
	for _, pkg := range packageSuggestions {
		insertText := fmt.Sprintf("package %s", pkg.name)
		items = append(items, CompletionItem{
			Label:      insertText,
			Kind:       protocol.ModuleCompletion,
			InsertText: insertText,
			Score:      pkg.score,
		})
	}

	return items, surrounding, nil
}

// packageCompletionSurrounding returns surrounding for package completion if a
// package completions can be suggested at a given cursor offset. A valid location
// for package completion is above any declarations or import statements.
func packageCompletionSurrounding(pgf *golang.ParsedGoFile, offset int) (*Selection, error) {
	m := pgf.Mapper
	// If the file lacks a package declaration, the parser will return an empty
	// AST. As a work-around, try to parse an expression from the file contents.
	fset := token.NewFileSet()
	expr, _ := parser.ParseExprFrom(fset, m.URI.Path(), pgf.Src, parser.Mode(0))
	if expr == nil {
		return nil, fmt.Errorf("unparseable file (%s)", m.URI)
	}
	tok := fset.File(expr.Pos())
	cursor := tok.Pos(offset)

	// If we were able to parse out an identifier as the first expression from
	// the file, it may be the beginning of a package declaration ("pack ").
	// We can offer package completions if the cursor is in the identifier.
	if name, ok := expr.(*ast.Ident); ok {
		if cursor >= name.Pos() && cursor <= name.End() {
			if !strings.HasPrefix(PACKAGE, name.Name) {
				return nil, fmt.Errorf("cursor in non-matching ident")
			}
			return &Selection{
				content: name.Name,
				cursor:  cursor,
				tokFile: tok,
				start:   name.Pos(),
				end:     name.End(),
				mapper:  m,
			}, nil
		}
	}

	// The file is invalid, but it contains an expression that we were able to
	// parse. We will use this expression to construct the cursor's
	// "surrounding".

	// First, consider the possibility that we have a valid "package" keyword
	// with an empty package name ("package "). "package" is parsed as an
	// *ast.BadDecl since it is a keyword. This logic would allow "package" to
	// appear on any line of the file as long as it's the first code expression
	// in the file.
	lines := strings.Split(string(pgf.Src), "\n")
	cursorLine := safetoken.Line(tok, cursor)
	if cursorLine <= 0 || cursorLine > len(lines) {
		return nil, fmt.Errorf("invalid line number")
	}
	if safetoken.StartPosition(fset, expr.Pos()).Line == cursorLine {
		words := strings.Fields(lines[cursorLine-1])
		if len(words) > 0 && words[0] == PACKAGE {
			content := PACKAGE
			// Account for spaces if there are any.
			if len(words) > 1 {
				content += " "
			}

			start := expr.Pos()
			end := token.Pos(int(expr.Pos()) + len(content) + 1)
			// We have verified that we have a valid 'package' keyword as our
			// first expression. Ensure that cursor is in this keyword or
			// otherwise fallback to the general case.
			if cursor >= start && cursor <= end {
				return &Selection{
					content: content,
					cursor:  cursor,
					tokFile: tok,
					start:   start,
					end:     end,
					mapper:  m,
				}, nil
			}
		}
	}

	// If the cursor is after the start of the expression, no package
	// declaration will be valid.
	if cursor > expr.Pos() {
		return nil, fmt.Errorf("cursor after expression")
	}

	// If the cursor is in a comment, don't offer any completions.
	if cursorInComment(tok, cursor, m.Content) {
		return nil, fmt.Errorf("cursor in comment")
	}

	// The surrounding range in this case is the cursor.
	return &Selection{
		content: "",
		tokFile: tok,
		start:   cursor,
		end:     cursor,
		cursor:  cursor,
		mapper:  m,
	}, nil
}

func cursorInComment(file *token.File, cursor token.Pos, src []byte) bool {
	var s scanner.Scanner
	s.Init(file, src, func(_ token.Position, _ string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if pos <= cursor && cursor <= token.Pos(int(pos)+len(lit)) {
			return tok == token.COMMENT
		}
		if tok == token.EOF {
			break
		}
	}
	return false
}

// packageNameCompletions returns name completions for a package clause using
// the current name as prefix.
func (c *completer) packageNameCompletions(ctx context.Context, fileURI protocol.DocumentURI, name *ast.Ident) error {
	cursor := int(c.pos - name.NamePos)
	if cursor < 0 || cursor > len(name.Name) {
		return errors.New("cursor is not in package name identifier")
	}

	c.completionContext.packageCompletion = true

	prefix := name.Name[:cursor]
	packageSuggestions, err := packageSuggestions(ctx, c.snapshot, fileURI, prefix)
	if err != nil {
		return err
	}

	for _, pkg := range packageSuggestions {
		c.deepState.enqueue(pkg)
	}
	return nil
}

// packageSuggestions returns a list of packages from workspace packages that
// have the given prefix and are used in the same directory as the given
// file. This also includes test packages for these packages (<pkg>_test) and
// the directory name itself.
func packageSuggestions(ctx context.Context, snapshot *cache.Snapshot, fileURI protocol.DocumentURI, prefix string) (packages []candidate, err error) {
	active, err := snapshot.WorkspaceMetadata(ctx)
	if err != nil {
		return nil, err
	}

	toCandidate := func(name string, score float64) candidate {
		obj := types.NewPkgName(0, nil, name, types.NewPackage("", name))
		return candidate{obj: obj, name: name, detail: name, score: score}
	}

	matcher := fuzzy.NewMatcher(prefix)

	// Always try to suggest a main package
	defer func() {
		if score := float64(matcher.Score("main")); score > 0 {
			packages = append(packages, toCandidate("main", score*lowScore))
		}
	}()

	dirPath := filepath.Dir(fileURI.Path())
	dirName := filepath.Base(dirPath)
	if !isValidDirName(dirName) {
		return packages, nil
	}
	pkgName := convertDirNameToPkgName(dirName)

	seenPkgs := make(map[golang.PackageName]struct{})

	// The `go` command by default only allows one package per directory but we
	// support multiple package suggestions since gopls is build system agnostic.
	for _, mp := range active {
		if mp.Name == "main" || mp.Name == "" {
			continue
		}
		if _, ok := seenPkgs[mp.Name]; ok {
			continue
		}

		// Only add packages that are previously used in the current directory.
		var relevantPkg bool
		for _, uri := range mp.CompiledGoFiles {
			if filepath.Dir(uri.Path()) == dirPath {
				relevantPkg = true
				break
			}
		}
		if !relevantPkg {
			continue
		}

		// Add a found package used in current directory as a high relevance
		// suggestion and the test package for it as a medium relevance
		// suggestion.
		if score := float64(matcher.Score(string(mp.Name))); score > 0 {
			packages = append(packages, toCandidate(string(mp.Name), score*highScore))
		}
		seenPkgs[mp.Name] = struct{}{}

		testPkgName := mp.Name + "_test"
		if _, ok := seenPkgs[testPkgName]; ok || strings.HasSuffix(string(mp.Name), "_test") {
			continue
		}
		if score := float64(matcher.Score(string(testPkgName))); score > 0 {
			packages = append(packages, toCandidate(string(testPkgName), score*stdScore))
		}
		seenPkgs[testPkgName] = struct{}{}
	}

	// Add current directory name as a low relevance suggestion.
	if _, ok := seenPkgs[pkgName]; !ok {
		if score := float64(matcher.Score(string(pkgName))); score > 0 {
			packages = append(packages, toCandidate(string(pkgName), score*lowScore))
		}

		testPkgName := pkgName + "_test"
		if score := float64(matcher.Score(string(testPkgName))); score > 0 {
			packages = append(packages, toCandidate(string(testPkgName), score*lowScore))
		}
	}

	return packages, nil
}

// isValidDirName checks whether the passed directory name can be used in
// a package path. Requirements for a package path can be found here:
// https://golang.org/ref/mod#go-mod-file-ident.
func isValidDirName(dirName string) bool {
	if dirName == "" {
		return false
	}

	for i, ch := range dirName {
		if isLetter(ch) || isDigit(ch) {
			continue
		}
		if i == 0 {
			// Directory name can start only with '_'. '.' is not allowed in module paths.
			// '-' and '~' are not allowed because elements of package paths must be
			// safe command-line arguments.
			if ch == '_' {
				continue
			}
		} else {
			// Modules path elements can't end with '.'
			if isAllowedPunctuation(ch) && (i != len(dirName)-1 || ch != '.') {
				continue
			}
		}

		return false
	}
	return true
}

// convertDirNameToPkgName converts a valid directory name to a valid package name.
// It leaves only letters and digits. All letters are mapped to lower case.
func convertDirNameToPkgName(dirName string) golang.PackageName {
	var buf bytes.Buffer
	for _, ch := range dirName {
		switch {
		case isLetter(ch):
			buf.WriteRune(unicode.ToLower(ch))

		case buf.Len() != 0 && isDigit(ch):
			buf.WriteRune(ch)
		}
	}
	return golang.PackageName(buf.String())
}

// isLetter and isDigit allow only ASCII characters because
// "Each path element is a non-empty string made of up ASCII letters,
// ASCII digits, and limited ASCII punctuation"
// (see https://golang.org/ref/mod#go-mod-file-ident).

func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z'
}

func isDigit(ch rune) bool {
	return '0' <= ch && ch <= '9'
}

func isAllowedPunctuation(ch rune) bool {
	return ch == '_' || ch == '-' || ch == '~' || ch == '.'
}
