// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

// This file defines routines to convert diagnostics from go list, go
// get, go/packages, parsing, type checking, and analysis into
// golang.Diagnostic form, and suggesting quick fixes.

import (
	"fmt"
	"go/scanner"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/protocol/command"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/bug"
)

func parseErrorDiagnostics(pkg *syntaxPackage, errList scanner.ErrorList) ([]*Diagnostic, error) {
	// The first parser error is likely the root cause of the problem.
	if errList.Len() <= 0 {
		return nil, fmt.Errorf("no errors in %v", errList)
	}
	e := errList[0]
	pgf, err := pkg.File(protocol.URIFromPath(e.Pos.Filename))
	if err != nil {
		return nil, err
	}
	rng, err := pgf.Mapper.OffsetRange(e.Pos.Offset, e.Pos.Offset)
	if err != nil {
		return nil, err
	}
	return []*Diagnostic{{
		URI:      pgf.URI,
		Range:    rng,
		Severity: protocol.SeverityError,
		Source:   ParseError,
		Message:  e.Msg,
	}}, nil
}

func goGetQuickFixes(moduleMode bool, uri protocol.DocumentURI, pkg string) []SuggestedFix {
	// Go get only supports module mode for now.
	if !moduleMode {
		return nil
	}
	title := fmt.Sprintf("go get package %v", pkg)
	cmd, err := command.NewGoGetPackageCommand(title, command.GoGetPackageArgs{
		URI:        uri,
		AddRequire: true,
		Pkg:        pkg,
	})
	if err != nil {
		bug.Reportf("internal error building 'go get package' fix: %v", err)
		return nil
	}
	return []SuggestedFix{SuggestedFixFromCommand(cmd, protocol.QuickFix)}
}

func editGoDirectiveQuickFix(moduleMode bool, uri protocol.DocumentURI, version string) []SuggestedFix {
	// Go mod edit only supports module mode.
	if !moduleMode {
		return nil
	}
	title := fmt.Sprintf("go mod edit -go=%s", version)
	cmd, err := command.NewEditGoDirectiveCommand(title, command.EditGoDirectiveArgs{
		URI:     uri,
		Version: version,
	})
	if err != nil {
		bug.Reportf("internal error constructing 'edit go directive' fix: %v", err)
		return nil
	}
	return []SuggestedFix{SuggestedFixFromCommand(cmd, protocol.QuickFix)}
}

// encodeDiagnostics gob-encodes the given diagnostics.
func encodeDiagnostics(srcDiags []*Diagnostic) []byte {
	var gobDiags []gobDiagnostic
	for _, srcDiag := range srcDiags {
		var gobFixes []gobSuggestedFix
		for _, srcFix := range srcDiag.SuggestedFixes {
			gobFix := gobSuggestedFix{
				Message:    srcFix.Title,
				ActionKind: srcFix.ActionKind,
			}
			for uri, srcEdits := range srcFix.Edits {
				for _, srcEdit := range srcEdits {
					gobFix.TextEdits = append(gobFix.TextEdits, gobTextEdit{
						Location: protocol.Location{
							URI:   uri,
							Range: srcEdit.Range,
						},
						NewText: []byte(srcEdit.NewText),
					})
				}
			}
			if srcCmd := srcFix.Command; srcCmd != nil {
				gobFix.Command = &gobCommand{
					Title:     srcCmd.Title,
					Command:   srcCmd.Command,
					Arguments: srcCmd.Arguments,
				}
			}
			gobFixes = append(gobFixes, gobFix)
		}
		var gobRelated []gobRelatedInformation
		for _, srcRel := range srcDiag.Related {
			gobRel := gobRelatedInformation(srcRel)
			gobRelated = append(gobRelated, gobRel)
		}
		gobDiag := gobDiagnostic{
			Location: protocol.Location{
				URI:   srcDiag.URI,
				Range: srcDiag.Range,
			},
			Severity:       srcDiag.Severity,
			Code:           srcDiag.Code,
			CodeHref:       srcDiag.CodeHref,
			Source:         string(srcDiag.Source),
			Message:        srcDiag.Message,
			SuggestedFixes: gobFixes,
			Related:        gobRelated,
			Tags:           srcDiag.Tags,
		}
		gobDiags = append(gobDiags, gobDiag)
	}
	return diagnosticsCodec.Encode(gobDiags)
}

// decodeDiagnostics decodes the given gob-encoded diagnostics.
func decodeDiagnostics(data []byte) []*Diagnostic {
	var gobDiags []gobDiagnostic
	diagnosticsCodec.Decode(data, &gobDiags)
	var srcDiags []*Diagnostic
	for _, gobDiag := range gobDiags {
		var srcFixes []SuggestedFix
		for _, gobFix := range gobDiag.SuggestedFixes {
			srcFix := SuggestedFix{
				Title:      gobFix.Message,
				ActionKind: gobFix.ActionKind,
			}
			for _, gobEdit := range gobFix.TextEdits {
				if srcFix.Edits == nil {
					srcFix.Edits = make(map[protocol.DocumentURI][]protocol.TextEdit)
				}
				srcEdit := protocol.TextEdit{
					Range:   gobEdit.Location.Range,
					NewText: string(gobEdit.NewText),
				}
				uri := gobEdit.Location.URI
				srcFix.Edits[uri] = append(srcFix.Edits[uri], srcEdit)
			}
			if gobCmd := gobFix.Command; gobCmd != nil {
				srcFix.Command = &protocol.Command{
					Title:     gobCmd.Title,
					Command:   gobCmd.Command,
					Arguments: gobCmd.Arguments,
				}
			}
			srcFixes = append(srcFixes, srcFix)
		}
		var srcRelated []protocol.DiagnosticRelatedInformation
		for _, gobRel := range gobDiag.Related {
			srcRel := protocol.DiagnosticRelatedInformation(gobRel)
			srcRelated = append(srcRelated, srcRel)
		}
		srcDiag := &Diagnostic{
			URI:            gobDiag.Location.URI,
			Range:          gobDiag.Location.Range,
			Severity:       gobDiag.Severity,
			Code:           gobDiag.Code,
			CodeHref:       gobDiag.CodeHref,
			Source:         DiagnosticSource(gobDiag.Source),
			Message:        gobDiag.Message,
			Tags:           gobDiag.Tags,
			Related:        srcRelated,
			SuggestedFixes: srcFixes,
		}
		srcDiags = append(srcDiags, srcDiag)
	}
	return srcDiags
}

// toSourceDiagnostic converts a gobDiagnostic to "source" form.
func toSourceDiagnostic(srcAnalyzer *settings.Analyzer, gobDiag *gobDiagnostic) *Diagnostic {
	var related []protocol.DiagnosticRelatedInformation
	for _, gobRelated := range gobDiag.Related {
		related = append(related, protocol.DiagnosticRelatedInformation(gobRelated))
	}

	severity := srcAnalyzer.Severity
	if severity == 0 {
		severity = protocol.SeverityWarning
	}

	diag := &Diagnostic{
		URI:      gobDiag.Location.URI,
		Range:    gobDiag.Location.Range,
		Severity: severity,
		Code:     gobDiag.Code,
		CodeHref: gobDiag.CodeHref,
		Source:   DiagnosticSource(gobDiag.Source),
		Message:  gobDiag.Message,
		Related:  related,
		Tags:     srcAnalyzer.Tag,
	}

	// If the fixes only delete code, assume that the diagnostic is reporting dead code.
	if onlyDeletions(diag.SuggestedFixes) {
		diag.Tags = append(diag.Tags, protocol.Unnecessary)
	}
	return diag
}

// onlyDeletions returns true if fixes is non-empty and all of the suggested
// fixes are deletions.
func onlyDeletions(fixes []SuggestedFix) bool {
	for _, fix := range fixes {
		if fix.Command != nil {
			return false
		}
		for _, edits := range fix.Edits {
			for _, edit := range edits {
				if edit.NewText != "" {
					return false
				}
				if protocol.ComparePosition(edit.Range.Start, edit.Range.End) == 0 {
					return false
				}
			}
		}
	}
	return len(fixes) > 0
}
