// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package source

import (
	"context"

	"cuelang.org/go/internal/golangorgx/gopls/lsp/cache"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/progress"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/util/maps"
)

// Analyze reports go/analysis-framework diagnostics in the specified package.
//
// If the provided tracker is non-nil, it may be used to provide notifications
// of the ongoing analysis pass.
func Analyze(ctx context.Context, snapshot *cache.Snapshot, pkgIDs map[PackageID]unit, tracker *progress.Tracker) (map[protocol.DocumentURI][]*cache.Diagnostic, error) {
	// Exit early if the context has been canceled. This also protects us
	// from a race on Options, see golang/go#36699.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	options := snapshot.Options()
	categories := []map[string]*settings.Analyzer{
		options.DefaultAnalyzers,
		options.StaticcheckAnalyzers,
		options.TypeErrorAnalyzers,
	}

	var analyzers []*settings.Analyzer
	for _, cat := range categories {
		for _, a := range cat {
			analyzers = append(analyzers, a)
		}
	}

	analysisDiagnostics, err := snapshot.Analyze(ctx, pkgIDs, analyzers, tracker)
	if err != nil {
		return nil, err
	}
	byURI := func(d *cache.Diagnostic) protocol.DocumentURI { return d.URI }
	return maps.Group(analysisDiagnostics, byURI), nil
}
