// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mod provides core features related to go.mod file
// handling for use by Go editors and tools.
package mod

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/cache"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/command"
	"cuelang.org/go/internal/golangorgx/gopls/lsp/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
)

// ParseDiagnostics returns diagnostics from parsing the go.mod files in the workspace.
func ParseDiagnostics(ctx context.Context, snapshot *cache.Snapshot) (map[protocol.DocumentURI][]*cache.Diagnostic, error) {
	ctx, done := event.Start(ctx, "mod.Diagnostics", snapshot.Labels()...)
	defer done()

	return collectDiagnostics(ctx, snapshot, ModParseDiagnostics)
}

// Diagnostics returns diagnostics from running go mod tidy.
func TidyDiagnostics(ctx context.Context, snapshot *cache.Snapshot) (map[protocol.DocumentURI][]*cache.Diagnostic, error) {
	ctx, done := event.Start(ctx, "mod.Diagnostics", snapshot.Labels()...)
	defer done()

	return collectDiagnostics(ctx, snapshot, ModTidyDiagnostics)
}

// UpgradeDiagnostics returns upgrade diagnostics for the modules in the
// workspace with known upgrades.
func UpgradeDiagnostics(ctx context.Context, snapshot *cache.Snapshot) (map[protocol.DocumentURI][]*cache.Diagnostic, error) {
	ctx, done := event.Start(ctx, "mod.UpgradeDiagnostics", snapshot.Labels()...)
	defer done()

	return collectDiagnostics(ctx, snapshot, ModUpgradeDiagnostics)
}

func collectDiagnostics(ctx context.Context, snapshot *cache.Snapshot, diagFn func(context.Context, *cache.Snapshot, file.Handle) ([]*cache.Diagnostic, error)) (map[protocol.DocumentURI][]*cache.Diagnostic, error) {
	g, ctx := errgroup.WithContext(ctx)
	cpulimit := runtime.GOMAXPROCS(0)
	g.SetLimit(cpulimit)

	var mu sync.Mutex
	reports := make(map[protocol.DocumentURI][]*cache.Diagnostic)

	for _, uri := range snapshot.View().ModFiles() {
		uri := uri
		g.Go(func() error {
			fh, err := snapshot.ReadFile(ctx, uri)
			if err != nil {
				return err
			}
			diagnostics, err := diagFn(ctx, snapshot, fh)
			if err != nil {
				return err
			}
			for _, d := range diagnostics {
				mu.Lock()
				reports[d.URI] = append(reports[fh.URI()], d)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return reports, nil
}

// ModParseDiagnostics reports diagnostics from parsing the mod file.
func ModParseDiagnostics(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle) (diagnostics []*cache.Diagnostic, err error) {
	pm, err := snapshot.ParseMod(ctx, fh)
	if err != nil {
		if pm == nil || len(pm.ParseErrors) == 0 {
			return nil, err
		}
		return pm.ParseErrors, nil
	}
	return nil, nil
}

// ModTidyDiagnostics reports diagnostics from running go mod tidy.
func ModTidyDiagnostics(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle) ([]*cache.Diagnostic, error) {
	pm, err := snapshot.ParseMod(ctx, fh) // memoized
	if err != nil {
		return nil, nil // errors reported by ModDiagnostics above
	}

	tidied, err := snapshot.ModTidy(ctx, pm)
	if err != nil {
		if err != cache.ErrNoModOnDisk {
			// TODO(rfindley): the check for ErrNoModOnDisk was historically determined
			// to be benign, but may date back to the time when the Go command did not
			// have overlay support.
			//
			// See if we can pass the overlay to the Go command, and eliminate this guard..
			event.Error(ctx, fmt.Sprintf("tidy: diagnosing %s", pm.URI), err)
		}
		return nil, nil
	}
	return tidied.Diagnostics, nil
}

// ModUpgradeDiagnostics adds upgrade quick fixes for individual modules if the upgrades
// are recorded in the view.
func ModUpgradeDiagnostics(ctx context.Context, snapshot *cache.Snapshot, fh file.Handle) (upgradeDiagnostics []*cache.Diagnostic, err error) {
	pm, err := snapshot.ParseMod(ctx, fh)
	if err != nil {
		// Don't return an error if there are parse error diagnostics to be shown, but also do not
		// continue since we won't be able to show the upgrade diagnostics.
		if pm != nil && len(pm.ParseErrors) != 0 {
			return nil, nil
		}
		return nil, err
	}

	upgrades := snapshot.ModuleUpgrades(fh.URI())
	for _, req := range pm.File.Require {
		ver, ok := upgrades[req.Mod.Path]
		if !ok || req.Mod.Version == ver {
			continue
		}
		rng, err := pm.Mapper.OffsetRange(req.Syntax.Start.Byte, req.Syntax.End.Byte)
		if err != nil {
			return nil, err
		}
		// Upgrade to the exact version we offer the user, not the most recent.
		title := fmt.Sprintf("%s%v", upgradeCodeActionPrefix, ver)
		cmd, err := command.NewUpgradeDependencyCommand(title, command.DependencyArgs{
			URI:        fh.URI(),
			AddRequire: false,
			GoCmdArgs:  []string{req.Mod.Path + "@" + ver},
		})
		if err != nil {
			return nil, err
		}
		upgradeDiagnostics = append(upgradeDiagnostics, &cache.Diagnostic{
			URI:            fh.URI(),
			Range:          rng,
			Severity:       protocol.SeverityInformation,
			Source:         cache.UpgradeNotification,
			Message:        fmt.Sprintf("%v can be upgraded", req.Mod.Path),
			SuggestedFixes: []cache.SuggestedFix{cache.SuggestedFixFromCommand(cmd, protocol.QuickFix)},
		})
	}

	return upgradeDiagnostics, nil
}

const upgradeCodeActionPrefix = "Upgrade to "

func sortedKeys(m map[string]bool) []string {
	ret := make([]string, 0, len(m))
	for k := range m {
		ret = append(ret, k)
	}
	sort.Strings(ret)
	return ret
}

// href returns the url for the vulnerability information.
// Eventually we should retrieve the url embedded in the osv.Entry.
// While vuln.go.dev is under development, this always returns
// the page in pkg.go.dev.
func href(vulnID string) string {
	return fmt.Sprintf("https://pkg.go.dev/vuln/%s", vulnID)
}

func getUpgradeCodeAction(fh file.Handle, req *modfile.Require, version string) (protocol.Command, error) {
	cmd, err := command.NewUpgradeDependencyCommand(upgradeTitle(version), command.DependencyArgs{
		URI:        fh.URI(),
		AddRequire: false,
		GoCmdArgs:  []string{req.Mod.Path + "@" + version},
	})
	if err != nil {
		return protocol.Command{}, err
	}
	return cmd, nil
}

func upgradeTitle(fixedVersion string) string {
	title := fmt.Sprintf("%s%v", upgradeCodeActionPrefix, fixedVersion)
	return title
}

// SelectUpgradeCodeActions takes a list of code actions for a required module
// and returns a more selective list of upgrade code actions,
// where the code actions have been deduped. Code actions unrelated to upgrade
// are deduplicated by the name.
func SelectUpgradeCodeActions(actions []protocol.CodeAction) []protocol.CodeAction {
	if len(actions) <= 1 {
		return actions // return early if no sorting necessary
	}
	var versionedUpgrade, latestUpgrade, resetAction protocol.CodeAction
	var chosenVersionedUpgrade string
	var selected []protocol.CodeAction

	seenTitles := make(map[string]bool)

	for _, action := range actions {
		if strings.HasPrefix(action.Title, upgradeCodeActionPrefix) {
			if v := getUpgradeVersion(action); v == "latest" && latestUpgrade.Title == "" {
				latestUpgrade = action
			} else if versionedUpgrade.Title == "" || semver.Compare(v, chosenVersionedUpgrade) > 0 {
				chosenVersionedUpgrade = v
				versionedUpgrade = action
			}
		} else if strings.HasPrefix(action.Title, "Reset govulncheck") {
			resetAction = action
		} else if !seenTitles[action.Command.Title] {
			seenTitles[action.Command.Title] = true
			selected = append(selected, action)
		}
	}
	if versionedUpgrade.Title != "" {
		selected = append(selected, versionedUpgrade)
	}
	if latestUpgrade.Title != "" {
		selected = append(selected, latestUpgrade)
	}
	if resetAction.Title != "" {
		selected = append(selected, resetAction)
	}
	return selected
}

func getUpgradeVersion(p protocol.CodeAction) string {
	return strings.TrimPrefix(p.Title, upgradeCodeActionPrefix)
}
