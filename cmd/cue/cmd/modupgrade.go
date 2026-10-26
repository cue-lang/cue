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
	"strings"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/tools/fix"
	"github.com/spf13/cobra"
)

func newModUpgradeCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade [flags] <version>",
		Short: "upgrade the module to a new language version",
		Long: `Upgrade moves the module to language version <version>: it rewrites the
module's files for the syntax and semantics of that version, and then
records the version in cue.mod/module.cue.

The files rewritten are the CUE files of the module's own tree, those
of the packages ./... denotes from the module root and of _ and testdata
directories, which can still be imported, plus every CUE file under
cue.mod/gen, cue.mod/pkg and cue.mod/usr, which the module evaluates at
its own language version. They are found by walking the module and
rewritten one package at a time, without loading: a package whose import
cannot be resolved is rewritten all the same. A nested module, a
directory with its own cue.mod, is left alone and upgrades on its own.

A symbolic link to a file is read through: a target inside the module is
converted once, under its own path. A link to a directory is not
followed, and a link to a file outside the module or inside a nested
module is not written through; the loader still reads such files, so
they fail the upgrade like a file that cannot be converted. A link that
does not resolve is skipped. cue.mod itself may be a link; it is read
through, as the loader reads it.

A file the parser rejects at the module's current version is skipped and
reported, and does not stop the upgrade; the module could not evaluate
it before either. A file that parses but cannot be rewritten, or whose
rewritten form the new version does not accept, is a failure: nothing is
then written and the version is left as it is, so the module never
claims a version its files do not satisfy. The report names the files
and the way through, which takes two steps:

	cue fix -f --exp=<experiments>

converts every file that converts and leaves the version alone, so the
module keeps evaluating as it is; the files it reports are then fixed by
hand, and running the upgrade again finds nothing left to convert and
records the version. --force is the shortcut that skips the second step:
it writes the files that converted and bumps the version anyway, leaving
the files it reports invalid at the new version until they are fixed.

` + fixCommentsHelp + `The summary names the files with a TODO for review.

Fixes are applied on a best-effort basis; check the result, for instance
by comparing the output of cue export before and after. A module whose
cue.mod/module.cue has no language version must run cue mod fix first.
`,
		RunE: mkRunE(c, runModUpgrade),
		Args: cobra.ExactArgs(1),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"write the files that converted and bump the version even when some file could not be converted")

	return cmd
}

func runModUpgrade(cmd *Command, args []string) error {
	target := args[0]
	if !semver.IsValid(target) {
		return fmt.Errorf("invalid version %q; must be valid semantic version (see http://semver.org)", target)
	}
	if latest := cueversion.LanguageVersion(); semver.Compare(target, latest) > 0 {
		return fmt.Errorf("language version %s is too new for this cue, which supports up to %s", target, latest)
	}

	root, err := findModuleRoot()
	if err != nil {
		return err
	}
	mf, modData, version, err := parseModuleFile(root)
	if err != nil {
		return err
	}
	switch semver.Compare(target, version) {
	case 0:
		fmt.Fprintf(cmd.ErrOrStderr(), "module is already at %s\n", version)
		return nil
	case -1:
		return fmt.Errorf("version %s is older than the module's %s", target, version)
	}
	force := flagForce.Bool(cmd)

	results, runErr := rewriteModule(root, mf, version, target, fix.UpgradeVersion(target))

	// The report goes to stderr without flipping the exit status: skipped
	// files and review notes are information, and only a failure below
	// makes the command fail.
	stderr := cmd.ErrOrStderr()
	skipped, failed := reportRewrites(stderr, results)
	if runErr != nil {
		// Something is wrong with the run as a whole, such as an experiment
		// that cannot apply; the files' own diagnostics are printed above.
		return runErr
	}

	// The review notice names what is on disk, so it follows every write.
	reviewNotice := func() {
		if marked := markedFiles(results); len(marked) > 0 {
			fmt.Fprintf(stderr, "mod upgrade: review the TODO(cue-fix) comments the rewrite added in: %s\n", strings.Join(marked, ", "))
		}
	}

	if failed > 0 && !force {
		// The way through is in two steps: convert everything that
		// converts without touching the version, fix the rest by hand,
		// run the upgrade again. --force is the shortcut that skips the
		// second step at the price of a module its version does not fit.
		fmt.Fprintf(stderr, "mod upgrade: %d file(s) could not be converted; nothing written, language version left at %s\n", failed, version)
		exps := upgradeFixes(version, target)
		switch {
		case len(exps) == 0:
			fmt.Fprintf(stderr, "    fix the files above by hand, then run the upgrade again\n")
		case canCarryAttributes(version, exps):
			fmt.Fprintf(stderr, "    to convert the other files now and stay at %s, then fix the files above by hand and run the upgrade again:\n", version)
			fmt.Fprintf(stderr, "        cue fix -f --exp=%s\n", strings.Join(exps, ","))
		default:
			fmt.Fprintf(stderr, "    %s cannot carry @experiment attributes for %s: fix the files above by hand, then run the upgrade again\n", version, strings.Join(exps, ","))
		}
		fmt.Fprintf(stderr, "    to upgrade anyway, leaving those files invalid at %s:\n", target)
		fmt.Fprintf(stderr, "        cue mod upgrade --force %s\n", target)
		return ErrPrintedError
	}

	// Everything is decided; write the files, and the module file last,
	// only once every file it vouches for is in place.
	if n := writeRewrites(stderr, results); n > 0 {
		fmt.Fprintf(stderr, "mod upgrade: %d file(s) could not be written; language version left at %s\n", n, version)
		reviewNotice()
		return ErrPrintedError
	}
	if mf.Language == nil {
		mf.Language = &modfile.Language{}
	}
	mf.Language.Version = target
	data, err := modfile.Format(mf)
	if err != nil {
		return fmt.Errorf("failed to format module file: %w", err)
	}
	if !writeUpgradedModuleFile(stderr, root, modData, data) {
		fmt.Fprintf(stderr, "mod upgrade: 1 file(s) could not be written; language version left at %s\n", version)
		reviewNotice()
		return ErrPrintedError
	}

	switch {
	case failed > 0 && skipped > 0:
		fmt.Fprintf(stderr, "mod upgrade: %d file(s) could not be converted, %d skipped; language version set to %s (--force)\n", failed, skipped, target)
	case failed > 0:
		fmt.Fprintf(stderr, "mod upgrade: %d file(s) could not be converted; language version set to %s (--force)\n", failed, target)
	case skipped > 0:
		fmt.Fprintf(stderr, "mod upgrade: language version set to %s; %d file(s) skipped\n", target, skipped)
	}
	reviewNotice()
	if failed > 0 {
		return ErrPrintedError
	}
	return nil
}

// upgradeFixes names the experiments an upgrade from version to target
// rewrites files for: those the lifecycle selects for the upgrade (stable
// at some version, not yet at version, available at target) that tools/fix
// has a rewrite for. The lifecycle also selects experiments that became
// stable without changing any syntax, and a test-only one; naming them in
// a cue fix command would only puzzle.
func upgradeFixes(version, target string) []string {
	var exps []string
	for _, exp := range cueexperiment.GetUpgradable(version, target) {
		switch exp {
		case "explicitopen", "aliasv2":
			exps = append(exps, exp)
		}
	}
	return exps
}

// canCarryAttributes reports whether a file at language version version
// may name every one of exps in an @experiment attribute: the form cue fix
// --exp writes, which exists from each experiment's preview version until
// it is stable.
func canCarryAttributes(version string, exps []string) bool {
	if len(exps) == 0 {
		return false
	}
	for _, exp := range exps {
		if cueexperiment.CanApplyFix(exp, version, version) != nil {
			return false
		}
	}
	return true
}
