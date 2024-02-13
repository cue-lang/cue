// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/gopls/vulncheck"
	"cuelang.org/go/internal/golangorgx/gopls/vulncheck/govulncheck"
	"cuelang.org/go/internal/golangorgx/gopls/vulncheck/osv"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

func Hover(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle, position protocol.Position) (*protocol.Hover, error) {
	var found bool
	for _, uri := range snapshot.View().ModFiles() {
		if fh.URI() == uri {
			found = true
			break
		}
	}

	// We only provide hover information for the view's go.mod files.
	if !found {
		return nil, nil
	}

	ctx, done := event.Start(ctx, "mod.Hover")
	defer done()

	// Get the position of the cursor.
	pm, err := snapshot.ParseMod(ctx, fh)
	if err != nil {
		return nil, fmt.Errorf("getting modfile handle: %w", err)
	}
	offset, err := pm.Mapper.PositionOffset(position)
	if err != nil {
		return nil, fmt.Errorf("computing cursor position: %w", err)
	}

	// If the cursor position is on a module statement
	if hover, ok := hoverOnModuleStatement(ctx, pm, offset, snapshot, fh); ok {
		return hover, nil
	}
	return hoverOnRequireStatement(ctx, pm, offset, snapshot, fh)
}

func hoverOnRequireStatement(ctx context.Context, pm *cache.ParsedModule, offset int, snapshot *cache.Snapshot, fh file.Handle) (*protocol.Hover, error) {
	// Confirm that the cursor is at the position of a require statement.
	var req *modfile.Require
	var startOffset, endOffset int
	for _, r := range pm.File.Require {
		dep := []byte(r.Mod.Path)
		s, e := r.Syntax.Start.Byte, r.Syntax.End.Byte
		i := bytes.Index(pm.Mapper.Content[s:e], dep)
		if i == -1 {
			continue
		}
		// Shift the start position to the location of the
		// dependency within the require statement.
		startOffset, endOffset = s+i, e
		if startOffset <= offset && offset <= endOffset {
			req = r
			break
		}
	}
	// TODO(hyangah): find position for info about vulnerabilities in Go

	// The cursor position is not on a require statement.
	if req == nil {
		return nil, nil
	}

	// Get the vulnerability info.
	fromGovulncheck := true
	vs := snapshot.Vulnerabilities(fh.URI())[fh.URI()]
	if vs == nil && snapshot.Options().Vulncheck == settings.ModeVulncheckImports {
		var err error
		vs, err = snapshot.ModVuln(ctx, fh.URI())
		if err != nil {
			return nil, err
		}
		fromGovulncheck = false
	}
	affecting, nonaffecting, osvs := lookupVulns(vs, req.Mod.Path, req.Mod.Version)

	// Get the `go mod why` results for the given file.
	why, err := snapshot.ModWhy(ctx, fh)
	if err != nil {
		return nil, err
	}
	explanation, ok := why[req.Mod.Path]
	if !ok {
		return nil, nil
	}

	// Get the range to highlight for the hover.
	// TODO(hyangah): adjust the hover range to include the version number
	// to match the diagnostics' range.
	rng, err := pm.Mapper.OffsetRange(startOffset, endOffset)
	if err != nil {
		return nil, err
	}
	options := snapshot.Options()
	isPrivate := snapshot.IsGoPrivatePath(req.Mod.Path)
	header := formatHeader(req.Mod.Path, options)
	explanation = formatExplanation(explanation, req, options, isPrivate)
	vulns := formatVulnerabilities(affecting, nonaffecting, osvs, options, fromGovulncheck)

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  options.PreferredContentFormat,
			Value: header + vulns + explanation,
		},
		Range: rng,
	}, nil
}

func hoverOnModuleStatement(ctx context.Context, pm *cache.ParsedModule, offset int, snapshot *cache.Snapshot, fh file.Handle) (*protocol.Hover, bool) {
	module := pm.File.Module
	if module == nil {
		return nil, false // no module stmt
	}
	if offset < module.Syntax.Start.Byte || offset > module.Syntax.End.Byte {
		return nil, false // cursor not in module stmt
	}

	rng, err := pm.Mapper.OffsetRange(module.Syntax.Start.Byte, module.Syntax.End.Byte)
	if err != nil {
		return nil, false
	}
	fromGovulncheck := true
	vs := snapshot.Vulnerabilities(fh.URI())[fh.URI()]

	if vs == nil && snapshot.Options().Vulncheck == settings.ModeVulncheckImports {
		vs, err = snapshot.ModVuln(ctx, fh.URI())
		if err != nil {
			return nil, false
		}
		fromGovulncheck = false
	}
	modpath := "stdlib"
	goVersion := snapshot.View().GoVersionString()
	affecting, nonaffecting, osvs := lookupVulns(vs, modpath, goVersion)
	options := snapshot.Options()
	vulns := formatVulnerabilities(affecting, nonaffecting, osvs, options, fromGovulncheck)

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  options.PreferredContentFormat,
			Value: vulns,
		},
		Range: rng,
	}, true
}

func formatHeader(modpath string, options *settings.Options) string {
	var b strings.Builder
	// Write the heading as an H3.
	b.WriteString("#### " + modpath)
	if options.PreferredContentFormat == protocol.Markdown {
		b.WriteString("\n\n")
	} else {
		b.WriteRune('\n')
	}
	return b.String()
}

func lookupVulns(vulns *vulncheck.Result, modpath, version string) (affecting, nonaffecting []*govulncheck.Finding, osvs map[string]*osv.Entry) {
	if vulns == nil || len(vulns.Entries) == 0 {
		return nil, nil, nil
	}
	for _, finding := range vulns.Findings {
		vuln, typ := foundVuln(finding)
		if vuln.Module != modpath {
			continue
		}
		// It is possible that the source code was changed since the last
		// govulncheck run and information in the `vulns` info is stale.
		// For example, imagine that a user is in the middle of updating
		// problematic modules detected by the govulncheck run by applying
		// quick fixes. Stale diagnostics can be confusing and prevent the
		// user from quickly locating the next module to fix.
		// Ideally we should rerun the analysis with the updated module
		// dependencies or any other code changes, but we are not yet
		// in the position of automatically triggering the analysis
		// (govulncheck can take a while). We also don't know exactly what
		// part of source code was changed since `vulns` was computed.
		// As a heuristic, we assume that a user upgrades the affecting
		// module to the version with the fix or the latest one, and if the
		// version in the require statement is equal to or higher than the
		// fixed version, skip the vulnerability information in the hover.
		// Eventually, the user has to rerun govulncheck.
		if finding.FixedVersion != "" && semver.IsValid(version) && semver.Compare(finding.FixedVersion, version) <= 0 {
			continue
		}
		switch typ {
		case vulnCalled:
			affecting = append(affecting, finding)
		case vulnImported:
			nonaffecting = append(nonaffecting, finding)
		}
	}

	// Remove affecting elements from nonaffecting.
	// An OSV entry can appear in both lists if an OSV entry covers
	// multiple packages imported but not all vulnerable symbols are used.
	// The current wording of hover message doesn't clearly
	// present this case well IMO, so let's skip reporting nonaffecting.
	if len(affecting) > 0 && len(nonaffecting) > 0 {
		affectingSet := map[string]bool{}
		for _, f := range affecting {
			affectingSet[f.OSV] = true
		}
		n := 0
		for _, v := range nonaffecting {
			if !affectingSet[v.OSV] {
				nonaffecting[n] = v
				n++
			}
		}
		nonaffecting = nonaffecting[:n]
	}
	sort.Slice(nonaffecting, func(i, j int) bool { return nonaffecting[i].OSV < nonaffecting[j].OSV })
	sort.Slice(affecting, func(i, j int) bool { return affecting[i].OSV < affecting[j].OSV })
	return affecting, nonaffecting, vulns.Entries
}

func fixedVersion(fixed string) string {
	if fixed == "" {
		return "No fix is available."
	}
	return "Fixed in " + fixed + "."
}

func formatVulnerabilities(affecting, nonaffecting []*govulncheck.Finding, osvs map[string]*osv.Entry, options *settings.Options, fromGovulncheck bool) string {
	if len(osvs) == 0 || (len(affecting) == 0 && len(nonaffecting) == 0) {
		return ""
	}
	byOSV := func(findings []*govulncheck.Finding) map[string][]*govulncheck.Finding {
		m := make(map[string][]*govulncheck.Finding)
		for _, f := range findings {
			m[f.OSV] = append(m[f.OSV], f)
		}
		return m
	}
	affectingByOSV := byOSV(affecting)
	nonaffectingByOSV := byOSV(nonaffecting)

	// TODO(hyangah): can we use go templates to generate hover messages?
	// Then, we can use a different template for markdown case.
	useMarkdown := options.PreferredContentFormat == protocol.Markdown

	var b strings.Builder

	if len(affectingByOSV) > 0 {
		// TODO(hyangah): make the message more eyecatching (icon/codicon/color)
		if len(affectingByOSV) == 1 {
			fmt.Fprintf(&b, "\n**WARNING:** Found %d reachable vulnerability.\n", len(affectingByOSV))
		} else {
			fmt.Fprintf(&b, "\n**WARNING:** Found %d reachable vulnerabilities.\n", len(affectingByOSV))
		}
	}
	for id, findings := range affectingByOSV {
		fix := fixedVersion(findings[0].FixedVersion)
		pkgs := vulnerablePkgsInfo(findings, useMarkdown)
		osvEntry := osvs[id]

		if useMarkdown {
			fmt.Fprintf(&b, "- [**%v**](%v) %v%v\n%v\n", id, href(id), osvEntry.Summary, pkgs, fix)
		} else {
			fmt.Fprintf(&b, "  - [%v] %v (%v) %v%v\n", id, osvEntry.Summary, href(id), pkgs, fix)
		}
	}
	if len(nonaffecting) > 0 {
		if fromGovulncheck {
			fmt.Fprintf(&b, "\n**Note:** The project imports packages with known vulnerabilities, but does not call the vulnerable code.\n")
		} else {
			fmt.Fprintf(&b, "\n**Note:** The project imports packages with known vulnerabilities. Use `govulncheck` to check if the project uses vulnerable symbols.\n")
		}
	}
	for k, findings := range nonaffectingByOSV {
		fix := fixedVersion(findings[0].FixedVersion)
		pkgs := vulnerablePkgsInfo(findings, useMarkdown)
		osvEntry := osvs[k]

		if useMarkdown {
			fmt.Fprintf(&b, "- [%v](%v) %v%v\n%v\n", k, href(k), osvEntry.Summary, pkgs, fix)
		} else {
			fmt.Fprintf(&b, "  - [%v] %v (%v) %v\n%v\n", k, osvEntry.Summary, href(k), pkgs, fix)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func vulnerablePkgsInfo(findings []*govulncheck.Finding, useMarkdown bool) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, f := range findings {
		p := f.Trace[0].Package
		if !seen[p] {
			seen[p] = true
			if useMarkdown {
				b.WriteString("\n  * `")
			} else {
				b.WriteString("\n    ")
			}
			b.WriteString(p)
			if useMarkdown {
				b.WriteString("`")
			}
		}
	}
	return b.String()
}

func formatExplanation(text string, req *modfile.Require, options *settings.Options, isPrivate bool) string {
	text = strings.TrimSuffix(text, "\n")
	splt := strings.Split(text, "\n")
	length := len(splt)

	var b strings.Builder

	// If the explanation is 2 lines, then it is of the form:
	// # golang.org/x/text/encoding
	// (main module does not need package golang.org/x/text/encoding)
	if length == 2 {
		b.WriteString(splt[1])
		return b.String()
	}

	imp := splt[length-1] // import path
	reference := imp
	// See golang/go#36998: don't link to modules matching GOPRIVATE.
	if !isPrivate && options.PreferredContentFormat == protocol.Markdown {
		target := imp
		if strings.ToLower(options.LinkTarget) == "pkg.go.dev" {
			target = strings.Replace(target, req.Mod.Path, req.Mod.String(), 1)
		}
		reference = fmt.Sprintf("[%s](%s)", imp, cache.BuildLink(options.LinkTarget, target, ""))
	}
	b.WriteString("This module is necessary because " + reference + " is imported in")

	// If the explanation is 3 lines, then it is of the form:
	// # golang.org/x/tools
	// modtest
	// golang.org/x/tools/go/packages
	if length == 3 {
		msg := fmt.Sprintf(" `%s`.", splt[1])
		b.WriteString(msg)
		return b.String()
	}

	// If the explanation is more than 3 lines, then it is of the form:
	// # golang.org/x/text/language
	// rsc.io/quote
	// rsc.io/sampler
	// golang.org/x/text/language
	b.WriteString(":\n```text")
	dash := ""
	for _, imp := range splt[1 : length-1] {
		dash += "-"
		b.WriteString("\n" + dash + " " + imp)
	}
	b.WriteString("\n```")
	return b.String()
}
