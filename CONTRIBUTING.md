# Contribution Guide

The CUE project uses GitHub for all contributions: bug reports, feature
requests, proposals, and code are all coordinated through
[github.com/cue-lang/cue](https://github.com/cue-lang/cue).

## Start with an issue

The most valuable contribution you can make to CUE is a well-written issue:
a bug report with a [minimal
reproducer](https://github.com/cue-lang/cue/wiki/Creating-test-or-performance-reproducers)
(ideally a self-contained `testscript` archive which runs `cue`), or a
feature request that explains the problem you are trying to solve.
Agreeing on what to build — and whether to build it — is the hard part of
most changes. Once that agreement exists, implementing it is usually
quick, and a maintainer will often do so directly.

For that reason, please open an issue on the [issue
tracker](https://github.com/cue-lang/cue/issues) before writing code, and
agree on an approach there before sending a pull request; only trivial
changes such as typo fixes can skip this step. Discussing a change on the
issue tracker first gives everyone a chance to validate the design, helps
prevent duplication of effort, and ensures the idea fits the goals of the
language and tools. A pull request is not the place for high-level design
discussion.

An approach is agreed once a maintainer has said so on the issue, or has
labeled it [`NeedsFix`](#finding-an-issue-to-work-on). An issue which only
describes a goal, or which tracks broader future work, does not settle how
to implement it: describe the approach you have in mind on the issue, and a
maintainer will let you know whether to go ahead. We may not be able to
review a pull request whose approach was not agreed first, particularly a
large or AI-generated one.

You can also exchange ideas or feedback with other contributors in the
`#contributing` channel on [Slack](https://cuelang.org/s/slack) and
[Discord](https://cuelang.org/s/discord).

## Contributing without writing code

There are many valuable ways to contribute to CUE beyond code:

* Ask or answer questions via GitHub discussions, Slack, and Discord
* Raise issues such as bug reports or feature requests on GitHub
* Contribute thoughts and use cases to proposals. CUE can be and is
  being used in many varied different ways. Sharing experience reports helps
  to shape proposals and designs.
* Create content: share blog posts, tutorials, videos, meetup talks, etc
* Add your project to [Unity](https://cue.dev/products/unity/) to help us
  test changes to CUE

## Finding an issue to work on

Whether you already know what contribution to make, or you are searching for
an idea, the [issue tracker](https://cuelang.org/issues) is always the first
place to go. Issues are triaged to categorize them and manage the workflow.

Most issues will be marked with one of the following workflow labels (links
are to queries in the issue tracker):

- [**Triage**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ATriage):
  Requires review by one of the core project maintainers.
- [**NeedsInvestigation**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsInvestigation):
  The issue is not fully understood and requires analysis to understand the root
  cause.
- [**NeedsDecision**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsDecision):
  the issue is relatively well understood, but the CUE team hasn't yet decided
  the best way to address it, so please wait for a decision before writing
  code. If you are interested in working on an issue in this state, feel free
  to "ping" maintainers in the issue's comments if some time has passed
  without a decision.
- [**NeedsFix**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsFix):
  the issue is fully understood and the approach is agreed, so code can be
  written to fix it.
- [**help wanted**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3A"help+wanted"):
  project maintainers need input from someone who has experience or expertise to
  answer or progress this issue.
- [**good first issue**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3A"good+first+issue"):
  often combined with `NeedsFix`, `good first issue` indicates an issue is very
  likely a good candidate for someone
  looking to make their first code contribution.

## Contributing code

Code contributions are made via [GitHub Pull
Requests](https://docs.github.com/en/pull-requests). We assume you have a
basic understanding of [`git`](https://git-scm.com/) and
[Go](https://go.dev/) (1.26 or later).

Code review for the CUE project historically happened on GerritHub; links to
past code reviews (such as `https://cuelang.org/cl/NNN`) continue to work.

### Asserting a Developer Certificate of Origin

Contributions to the CUE project must be accompanied by a [Developer
Certificate of Origin](https://developercertificate.org/); we are using
version 1.1.

All commit messages must contain the `Signed-off-by` line with an email
address that matches the commit author. This line asserts the Developer
Certificate of Origin.

When committing, use the `--signoff` (or `-s`) flag:

```console
$ git commit -s
```

You can also [set up a prepare-commit-msg git
hook](#do-i-really-have-to-add-the--s-flag-to-each-commit) to not have to
supply the `-s` flag.

### Sending a change

Make sure the change you plan to make is covered by an issue whose approach
has been agreed; see [Start with an issue](#start-with-an-issue). Then
[fork](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/working-with-forks/fork-a-repo)
the CUE project and clone your fork locally.

Make your planned changes; a bug fix should include a regression test
which fails without the fix, typically based on the reproducer from the
issue. Then create a signed-off commit from the staged changes, following
the guidelines on [good commit messages](#good-commit-messages) and
[referencing the issue](#referencing-issues):

```console
$ git add file1 file2
$ git commit -s
```

Before sending the change out for review, run all the tests from the root of
the repository to ensure the changes don't break other packages or programs:

```console
$ go test ./...
```

Your change is now ready!
[Open a PR](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request)
in the usual way.

### One commit per PR

Some projects accept and encourage multiple commits in a single PR. The CUE
project treats a single commit as the unit of change: all PRs must contain
a single commit. This keeps each change self-contained, and encourages
unrelated changes to be broken into separate PRs.

To make changes requested during review while keeping a single commit,
amend the existing commit and force push:

```console
# PR is open, feedback received. Time to make some changes!

$ git add file1 file2   # stage the files we have added/removed/changed
$ git commit --amend    # amend the last commit
$ git push -f           # push the amended commit to your PR
```

The `-f` flag is required to force push your branch to GitHub: this
overrides a warning from `git` telling you that GitHub knows nothing about
the relationship between the original commit in your PR and the amended
commit.

If you accidentally create additional commits on your branch, you can
"squash" them into a single commit; see the GitHub documentation on [how to
squash commits](https://docs.github.com/en/desktop/managing-commits/squashing-commits-in-github-desktop).

### Review and merging

A maintainer will arrange for the continuous integration (CI) checks to run
against your PR; if any of them fail, you will be asked to address the
failures. A maintainer may also review the change and request adjustments.
Treat each review comment like a ticket: close it by implementing the
suggestion, or reply explaining why you have not, or what you have done
instead.

Once your change is approved, it will land on the `master` branch and the PR
will be closed as merged. Changes are rebased onto `master` as they land, so
the commit on `master` may differ from the one on your PR branch.

## Good commit messages

Commit messages in CUE follow a specific set of conventions, which we discuss
in this section.

Here is an example of a good one:

```
cue/ast/astutil: fix resolution bugs

This fixes several bugs and documentation bugs in
identifier resolution.

1. Resolution in comprehensions would resolve identifiers
   to themselves.

2. Label aliases now no longer bind to references outside the scope
   of the field. The compiler would catch this invalid bind and
   report an error, but it is better not to bind in the first place.

3. Remove some more mentions of Template labels.

4. Documentation for comprehensions was incorrect
   (Scope and Node were reversed).

5. Aliases X in `X=[string]: foo` should only be visible in foo.

Fixes #946
```

### First line

The first line of the change description is conventionally a short one-line
summary of the change, prefixed by the primary affected package
(`cue/ast/astutil` in the example above).

A rule of thumb is that it should be written so to complete the sentence "This
change modifies CUE to \_\_\_\_." That means it does not start with a capital
letter, is not a complete sentence, and actually summarizes the result of the
change.

Follow the first line by a blank line.

### Main content

The rest of the description elaborates and should provide context for the change
and explain what it does.  Write in complete sentences with correct punctuation,
just like for your comments in CUE.  Don't use HTML, Markdown, or any other
markup language.

### Referencing issues

Unless a change is trivial, its commit message should reference the issue
where its approach was [agreed](#start-with-an-issue).

The special notation `Fixes #12345` associates the change with issue 12345 in
the [CUE issue tracker](https://cuelang.org/issue/12345).  When this change is
eventually applied, the issue tracker will automatically mark the issue as
fixed.

If the change is a partial step towards the resolution of the issue, use the
notation `Updates #12345`.  This will leave a comment in the issue linking back
to the change, but it will not close the issue when the change is applied.

All issues are tracked in the main repository's issue tracker.
If you are sending a change against a subrepository, you must use the
fully-qualified syntax supported by GitHub to make sure the change is linked to
the issue in the main repository, not the subrepository (eg. `Fixes cue-lang/cue#999`).

## Miscellaneous topics

This section collects a number of other comments that are outside the
issue/edit/review process itself.

### Copyright headers

Files in the CUE repository don't list author names, both to avoid clutter and
to avoid having to keep the lists up to date.  Instead, your name will appear in
the [git change log](https://github.com/cue-lang/cue/commits/master)
and in [GitHub's contributor stats](https://github.com/cue-lang/cue/graphs/contributors)
when using an email address linked to a GitHub account.

New files that you contribute should use the standard copyright header
with the current year reflecting when they were added.
Do not update the copyright year for existing files that you change.

```
// Copyright 2018 The CUE Authors
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
```

### Quickly testing your changes

Running `go test ./...` for every single change to the code tree is
burdensome. Even though it is strongly suggested to run it before sending a
change, during the normal development cycle you may want to test only the
package you are developing, for example `go test ./cue`. The tests run
against the code in your working tree: you do not need to build or install
a new `cue` tool for the tests to pick up your changes.

### Reviewing code by others

As part of the review process reviewers can propose changes directly, by
attaching commits to a pull request. You can check out someone else's proposed
changes locally by following the GitHub documentation on [checking out pull
requests locally](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/checking-out-pull-requests-locally),
or with the [`gh` CLI](https://cli.github.com/):

```console
$ gh pr checkout 9999
```

### Do I really have to add the `-s` flag to each commit?

Earlier in this guide we explained the role the [Developer Certificate of
Origin](https://developercertificate.org/) plays in contributions to the CUE
project, and how `git commit -s` can be used to sign-off each commit. But:

* it's easy to forget the `-s` flag;
* it's not always possible/easy to fix up other tools that wrap the `git commit` step.

You can automate the sign-off step using a [`git`
hook](https://git-scm.com/book/en/v2/Customizing-Git-Git-Hooks). Run the
following commands in the root of a `git` repository where you want to
automatically sign-off each commit:

```
cat <<'EOD' > .git/hooks/prepare-commit-msg
#!/bin/sh

NAME=$(git config user.name)
EMAIL=$(git config user.email)

if [ -z "$NAME" ]; then
    echo "empty git config user.name"
    exit 1
fi

if [ -z "$EMAIL" ]; then
    echo "empty git config user.email"
    exit 1
fi

git interpret-trailers --if-exists doNothing --trailer \
    "Signed-off-by: $NAME <$EMAIL>" \
    --in-place "$1"
EOD
chmod +x .git/hooks/prepare-commit-msg
```

If you already have a `prepare-commit-msg` hook, adapt it accordingly. The `-s`
flag will now be implied every time a commit is created.

## Code of Conduct

Guidelines for participating in CUE community spaces and a reporting process for
handling issues can be found in the [Code of Conduct](https://cuelang.org/docs/reference/code-of-conduct/).
