---
name: backport
description: Backport queued fixes to the latest release branch. Collects the issues whose "backport" field is Queued in the CUE planning project, finds their commits on master, and cherry-picks them onto the newest release-branch.vX.Y. Use when asked to backport queued issues or to process the Backports project view.
---

# Backporting queued fixes

The queue lives in the "CUE planning" GitHub org project (number 37).
Its Backports view (https://github.com/orgs/cue-lang/projects/37/views/9)
shows items with the custom "backport" field set; a value of "Queued"
means the fix is awaiting a backport to the latest release branch.

Prerequisites: a clean working tree, and `git fetch origin` run first.
The project query needs the `read:project` scope; if it fails with an
INSUFFICIENT_SCOPES error, ask the user to run
`! gh auth refresh -h github.com -s read:project` and retry.

Place any temporary files under `.claude/tmp/`, and remove them when
done.

## 1. Determine the target release branch

The latest release branch, by version order:

```
git ls-remote --heads origin 'release-branch.*' |
  sed 's|.*refs/heads/||' | sort -V | tail -n1
```

## 2. Collect the queued issues

```
gh project item-list 37 --owner cue-lang --format json --limit 1000 |
  jq -r '.items[] | select(.backport == "Queued") |
    "\(.content.repository)#\(.content.number)\t\(.status)\t\(.title)"'
```

Keep only issues in this repository (cue-lang/cue); report any others
as out of scope.

## 3. Look for missed backport candidates

The project queue may be incomplete. Review the commits on master
that are not yet on the release branch:

```
git log --format='%h %s' --right-only --cherry-pick --no-merges \
  origin/<target>...origin/master
```

(`--cherry-pick` omits commits whose patch already landed on the
release branch.) Reading subjects, and full messages where a subject
is ambiguous, pick out the commits not covered by the queued issues
that are strong backport candidates: fixes for regressions affecting
the release being backported to, panic fixes, hang fixes, and any
other fix that should not wait for the next feature release. Skip
features, refactors, docs, CI, and dependency updates.

Present this list to the user and have them choose which, if any, to
backport alongside the queued issues; do not continue until they have
answered. Treat their picks like the commits of a queued issue in the
steps below — a pick may itself have companion commits (e.g. a
preceding regression-test commit), which must be included too.

## 4. Map each issue to its commits on master

A fix often spans several commits (e.g. a regression test commit
followed by the fix commit); all of them must be backported. For each
issue number N, search master:

```
git log origin/master --format='%H %s' -E \
  --grep='#N([^0-9]|$)' --grep='cuelang.org/issue/N'
```

Read each candidate's full message and keep the commits whose
`Fixes #N` / `Updates #N` / `For #N` line (including the
`cue-lang/cue#N` form) references the issue. A commit that merely
mentions the issue in passing prose does not belong; judge borderline
cases by whether the commit is part of the fix, and note such judgment
calls in the final report.

If an issue has no commits on master, its fix has likely not been
merged yet (its project status is usually not Done); list it as
skipped and move on.

## 5. Order and prune the commit list

Deduplicate the SHAs (one commit may fix several issues), then order
them oldest to newest as they appear on master:

```
git rev-list --reverse origin/master | grep -Ff picks.txt
```

where picks.txt holds one full SHA per line.

Drop any commit already backported: compare each pick's subject
against `git log --format=%s <merge-base>..origin/<target>`, ignoring
the `[<target>] ` subject prefix described below.

## 6. Check out the release branch

```
git checkout <target>
```

Cherry-pick directly on the local release branch — no separate work
branch. The checkout creates it tracking `origin/<target>` if it does
not exist yet; if it already exists, fast-forward it to
`origin/<target>` first.

## 7. Cherry-pick, oldest first

For each SHA in order:

```
git cherry-pick <sha>
GIT_EDITOR="sed -i '1s/^/[<target>] /'" git commit --amend
```

The amend adds the conventional subject prefix, e.g.
`[release-branch.v0.17] internal/mod: resolve module replacements...`.
Keep the rest of the message intact, including the original Change-Id
and Reviewed-on trailers: Gerrit identifies changes per branch, so the
same Change-Id opens a new CL on the release branch while linking it
to the original. Never strip or regenerate the Change-Id.

On a cherry-pick conflict, attempt a resolution as long as the result
is still the original fix in principle — typically context drift or
surrounding code that diverged on the release branch. Only when a
resolution would have to change what the fix does should you stop,
leave the cherry-pick in progress, and report which commit conflicts
and why.

Any commit that required conflict resolution must say so: as part of
the same amend that adds the subject prefix, append a brief paragraph
at the end of the message body (before the trailers) summarizing what
conflicted and how it was resolved.

## 8. Test each pick

Each backported commit must pass `go test ./...`. Golden outputs
(such as evaluator stats sections) often vary between branches, so
also run `CUE_UPDATE=1 go test ./...` and amend any resulting diffs
into the commit itself, then re-run `go test ./...` to confirm it
passes.

## 9. Report and stop

Do not mail, push, or edit the project's backport field — maintainers
flip it once the backport CLs land. Report: the queued issues, the
extra candidates the user chose in step 3, the commits picked per
issue in order, issues skipped (no commits found) or commits dropped
(already backported), any conflict resolutions or judgment calls, and
the test results.
