
# Contribution Guide


The CUE project welcomes all contributors, and there are many ways that you can
contribute that don't involve writing code!

This document guides you through the process of contributing to the CUE project.


### How can I contribute to the CUE project?

There are many ways that you can contribute to the CUE project
that don't involve writing code.

* Using CUE is a form of contributing! Especially when combined with raising
  issues, providing feedback, tell us what works well and what doesn't, pointing
out gaps etc.
* Adding your CUE-based project to [`unity`](https://github.com/cue-unity/unity)
  helps ensure that we don't create releases that unintentionally break
configurations, but also gives a wide variety of scenarios in which to test bug
fixes, performance improvements and the like.
* Asking questions via GitHub discussions/Slack. This might seem somewhat
  counterintuitive, but asking questions helps to identify gaps in
documentation, or poor signposting from the CUE homepage.
* Raising issues with bug reports and feature requests helps us to raise the
  quality of future CUE releases. In the case of bug reports not least because
it provides us with real-world test cases.
* Helping to manage issues and answer discussions. Sometimes referred to as
  "issue gardening", this helps to share the load of triaging new issues and
feature requests. Having issues presented in a familiar "shape", format and
voice is a _massive_ time saver when it comes to one of the core contributors
fixing a bug, or considering a new feature.
* Code contributions, the main focus of this guide. The CUE project is a little
  different from that used by other open source projects so we cover this
process in more detail below.
* Contributing thoughts and use cases use cases to proposals. CUE can be and is
  being used in many varied different ways. Sharing experience reports helps
to shape proposals and designs.
* Creating content. Whether it be writing blog posts, live streaming,
  tweeting...  creating content is a great way of growing the CUE community.
Different people have different ways of explaining things, and very often these
different styles appeal to different people. That said, if you think there is
core documentation or guides missing from the https://cuelang.org website
[please raise an issue to let us
know](https://github.com/cue-lang/cue/issues/new/choose): there is not
substitute for good core content, and it means others are then free to write
about more interesting use cases and applications for CUE.
* Holding community events. Whether they be virtual online events or
  (COVID-allowing) in-person meetups, sharing experiences about using CUE is a
very valuable way of learning for many.

Thank you to everyone who contributes to the CUE community in whatever form!
Whilst GitHub doesn't have a good means of tracking contributions outside of
code contributions, your contributions are greatly valued!


## Before contributing code

As with many open source projects, CUE uses the GitHub [issue
tracker](https://github.com/cue-lang/cue/issues) to not only track bugs, but
also coordinate work on new features, bugs, designs and proposals.  Given the
inherently distributed nature of open-source this coordination is important
because it very often serves as the main form of communication between
contributors.

### Check the issue tracker

Whether you already know what contribution to make, or you are searching for an
idea, the [issue tracker](https://cuelang.org/issues) is always the first place
to go.  Issues are triaged to categorize them and manage the workflow.

Most issues will be marked with one of the following workflow labels (links are
to queries in the issue tracker):

- [**Triage**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ATriage):
  Requires review by one of the core project maintainers.
- [**NeedsInvestigation**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsInvestigation):
  The issue is not fully understood and requires analysis to understand the root
cause.
- [**NeedsDecision**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsDecision):
  the issue is relatively well understood, but the CUE team hasn't yet decided
the best way to address it.  It would be better to wait for a decision before
writing code.  If you are interested on working on an issue in this state, feel
free to "ping" maintainers in the issue's comments if some time has passed
without a decision.
- [**NeedsFix**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsFix):
  the issue is fully understood and code can be written to fix it.
- [**help
  wanted**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3A"help+wanted"):
project maintainers need input from someone who has experience or expertise to
answer or progress this issue.
- [**good first
  issue**](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3A"good+first+issue"):
often combined with `NeedsFix`, `good first issue` indicates an issue is very
likely a good candidate for someone
looking to make their first code contribution.


### Open an issue for any new problem

Excluding very trivial changes, all contributions should be connected to an
existing issue.  Feel free to open one and discuss your plans.  This process
gives everyone a chance to validate the design, helps prevent duplication of
effort, and ensures that the idea fits inside the goals for the language and
tools.  It also checks that the design is sound before code is written; the code
review tool is not the place for high-level discussions.

Sensitive security-related issues should be reported to <a
href="mailto:security@cuelang.org">security@cuelang.org</a>.

## Becoming a code contributor

The code contribution process used by the CUE project is a little different from
that used by other open source projects.  We assume you have a basic
understanding of [`git`](https://git-scm.com/) and [Go](https://golang.org)
(1.16 or later).

The first thing to decide is whether you want to contribute a code change via
GitHub or GerritHub. Both workflows are fully supported, and whilst GerritHub is
used by the core project maintainers as the "source of truth", the GitHub Pull
Request workflow is 100% supported - contributors should feel entirely
comfortable contributing this way if they prefer.

Contributions via either workflow must be accompanied by a Developer Certificate
of Origin.

### Asserting a Developer Certificate of Origin

Contributions to the CUE project must be accompanied by a [Developer Certificate
of Origin](https://developercertificate.org/), the text of which is reproduced
here for convenience:

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

All commit messages must contain the `Signed-off-by` line with an email address
that matches the commit author. This line asserts the Developer Certificate of Origin.

When committing, use the `--signoff` (or `-s`) flag:

```
git commit -s
```

You can also [set up a prepare-commit-msg git
hook](#do-i-really-have-to-add-the--s-flag-to-each-commit) to not have to supply
the `-s` flag.

The explanations of the GitHub and GerritHub contribution workflows that follow
assume all commits you create are signed-off in this way.


## Preparing for GitHub Pull Request (PR) Contributions

First-time contributors that are already familiar with the <a
href="https://guides.github.com/introduction/flow/">GitHub flow</a> are
encouraged to use the same process for CUE contributions.  Even though CUE
maintainers use GerritHub for code review, the GitHub PR workflow is 100%
supported.

Here is a checklist of the steps to follow when contributing via GitHub PR
workflow:

- **Step 0**: Review the guidelines on [Good Commit
  Messages](#good-commit-messages), [The Review Process](#the-review-process)
and [Miscellaneous Topics](#miscellaneous-topics)
- **Step 1**: Create a GitHub account if you do not have one.
- **Step 2**:
  [Fork](https://docs.github.com/en/get-started/quickstart/fork-a-repo) the CUE
project, and clone your fork locally


That's it! You are now ready to send a change via GitHub, the subject of the
next section.



## Sending a change via GitHub

The GitHub documentation around [working with
forks](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/getting-started/about-collaborative-development-models)
is extensive so we will not cover that ground here.

Before making any changes it's a good idea to verify that you have a stable
baseline by running the tests:

```
$ go test ./...
```

Then make your planned changes and create a commit from the staged changes:

```
# Edit files
$ git add file1 file2
$ git commit -s
```

Notice as we explained above, the `-s` flag asserts the Developer Certificate of
Origin by adding a `Signed-off-by` line to a commit. When writing a commit
message, remember the guidelines on [good commit
messages](#good-commit-messages).

You’ve written and tested your code, but before sending code out for review, run
all the tests from the root of the repository to ensure the changes don’t break
other packages or programs:

```
$ go test ./...
```

Your change is now ready! [Submit a
PR](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request)
in the usual way.

Once your PR is submitted, a maintainer will trigger continuous integration (CI)
workflows to run and [review your proposed
change](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/reviewing-proposed-changes-in-a-pull-request).
The results from CI and the review might indicate further changes are required,
and this is where the CUE project differs from others:

### Making changes to a PR

Some projects accept and encourage multiple commits in a single PR. Either as a
way of breaking down the change into smaller parts, or simply as a record of the
various changes during the review process.

The CUE project follows the Gerrit model of a single commit being the unit of
change. Therefore, all PRs must only contain a single commit. But how does this
work if you need to make changes requested during the review process? Does this
not require you to create additional commits?

The easiest way to maintain a single commit is to amend an existing commit.
Rather misleadingly, this doesn't actually amend a commit, but instead creates a
new commit which is the result of combining the last commit and any new changes:

```
# PR is submitted, feedback received. Time to make some changes!

$ git add file1 file2   # stage the files we have added/removed/changed
$ git commit --amend    # amend the last commit
$ git push -f           # push the amended commit to your PR
```

The `-f` flag is required to force push your branch to GitHub: this overrides a
warning from `git` telling you that GitHub knows nothing about the relationship
between the original commit in your PR and the amended commit.

What happens if you accidentally create an additional commit and now have two
commits on your branch? No worries, you can "squash" commits on a branch to
create a single commit. See the GitHub documentation on [how to squash commits
with GitHub
Desktop](https://docs.github.com/en/desktop/contributing-and-collaborating-using-github-desktop/managing-commits/squashing-commits),
or using the [`git` command
interactively](https://medium.com/@slamflipstrom/a-beginners-guide-to-squashing-commits-with-git-rebase-8185cf6e62ec).

### PR approved!

With the review cycle complete, the CI checks green and your PR approved, it
will be imported into GerritHub and then submitted. Your PR will close
automatically as it is "merged" in GerritHub. Congratulations! You will have
made your first contribution to the CUE project.


## Preparing for GerritHub [CL](https://google.github.io/eng-practices/#terminology) Contributions

CUE maintainers use GerritHub for code review. It has a powerful review
interface with comments that are attributed to patchsets (versions of a change).
Orienting changes around a single commit allows for "stacked" changes, and also
encourages unrelated changes to be broken into separate CLs because the process
of creating and linking CLs is so easy.

For those more comfortable with contributing via GitHub PRs, please continue to
do so: the CUE project supports both workflows so that people have a choice.

For those who would like to contribute via GerritHub, read on!

### Overview

The first step in the GerritHub flow is registering as a CUE contributor and
configuring your environment. Here is a checklist of the required steps to
follow:

- **Step 0**: Review the guidelines on [Good Commit Messages](#good-commit-messages), [The Review Process](#the-review-process) and [Miscellaneous Topics](#miscellaneous-topics)
- **Step 1**: Decide which email address you want to use for contributions.
- **Step 2**: Set up a [GerritHub](http://gerrithub.io/) account.
- **Step 3**: Install `git-codereview`
- **Step 4**: Clone the CUE repository locally.


We cover steps 1-4 in more detail below.

### Step 1: Decide which email address you want to use for contributions

A contribution to CUE is made through a specific e-mail address.  Make sure to
use the same account throughout the process and for all your subsequent
contributions.  You may need to decide whether to use a personal address or a
corporate address.  The choice will depend on who will own the copyright for the
code that you will be writing and submitting.  You might want to discuss this
topic with your employer before deciding which account to use.

You also need to make sure that your `git` tool is configured to create commits
using your chosen e-mail address.  You can either configure Git globally (as a
default for all projects), or locally (for a single specific project).  You can
check the current configuration with this command:

```
$ git config --global user.email  # check current global config
$ git config user.email           # check current local config
```

To change the configured address:

```
$ git config --global user.email name@example.com   # change global config
$ git config user.email name@example.com            # change local config
```

### Step 2: Setup a GerritHub account

If you have not used GerritHub before, setting up an account is a simple
process:

- Visit [GerritHub](http://gerrithub.io/).
- Click "First Time Sign In".
- Click the green "Sign In" button, to sign in using your GitHub
  credentials.
- When prompted "Which level of GitHub access do you need?", choose
  "Default" and then click "Login."
- Click "Authorize gerritforge-ltd" on the GitHub auth page.
- Confirm account profile details and click "Next."

If you want to use SSH for authentication, SSH keys can be [configured in your
user profile](https://review.gerrithub.io/settings/#SSHKeys).

For HTTP Credentials, [generate a password via your user
profile](https://review.gerrithub.io/settings/#HTTPCredentials). Then use an
existing HTTP authentication mechanism like `.netrc`, macOS KeyChain, or some
other [credential helper](https://git-scm.com/docs/gitcredentials). If you have
any troubles with this step, please [raise an
issue](https://cuelang.org/issues/new).


### Step 3: Install the `git-codereview` command

Changes to CUE must be reviewed before they are accepted, no matter who makes
the change.  A custom `git` command called `git-codereview` simplifies sending
changes to Gerrit.

<!-- Keep the following in sync with cmd/cue/cmd/testdata/script/contributing_install*.txt -->

Install the `git-codereview` command by running,

```
$ go install golang.org/x/review/git-codereview@master
```

Make sure `git-codereview` is installed in your shell `PATH`, so that the
`git` command can find it.
Check that

```
$ git codereview help
```

prints help text, not an error.

On Windows, when using git-bash you must make sure that `git-codereview.exe` is
in your `git` exec-path.  Run `git --exec-path` to discover the right location
then create a symbolic link or just copy the executable from $GOPATH/bin to this
directory.

### Step 4: Clone the CUE repository locally

Visit https://review.gerrithub.io/admin/repos/cue-lang/cue, then click "SSH" or
"HTTP" depending on which authentication mechanism you configured in step 2.
Then copy and run the corresponding "Clone" command.

## Sending a change via GerritHub

Sending a change via GerritHub is quite different to the GitHub PR flow. At
first the differences might be jarring, but with practice the workflow is
incredibly intuitive and far more powerful when it comes to chains of "stacked"
changes.

### Step 1: Ensure you have a stable baseline

With a working directory of your local clone of the CUE repository, run the tests:

```
$ go test ./...
```

### Step 2: Prepare changes in a new branch

Each CUE change must be made in a branch, created from the `master` branch.  You
can use the normal `git` commands to create a branch and stage changes:


```
$ git checkout -b mybranch
$ [edit files...]
$ git add [files...]
```

To commit changes, instead of `git commit -s`, use `git codereview change -s`.


```
$ git codereview change -s
(opens $EDITOR)
```

You can edit the commit description in your favorite editor as usual.  The
`git` `codereview` `change` command will automatically add a unique Change-Id
line near the bottom.  That line is used by Gerrit to match successive uploads
of the same change.  Do not edit or delete it.  A Change-Id looks like this:


```
Change-Id: I2fbdbffb3aab626c4b6f56348861b7909e3e8990
```

The tool also checks that you've run `go` `fmt` over the source code, and that
the commit message follows the suggested format.


If you need to edit the files again, you can stage the new changes and re-run
`git codereview change -s`: each subsequent run will amend the existing commit
while preserving the Change-Id.

Make sure that you always keep a single commit in each branch.  If you add more
commits by mistake, you can use `git rebase` to [squash them
together](https://medium.com/@slamflipstrom/a-beginners-guide-to-squashing-commits-with-git-rebase-8185cf6e62ec)
into a single one.





### Step 3: Test your changes

You've written and tested your code, but before sending code out for review, run
all the tests for the whole tree to ensure the changes don't break other
packages or programs:


```
$ go test ./...
```


### Step 4: Send changes for review

Once the change is ready and tested over the whole tree, send it for review.
This is done with the `mail` sub-command which, despite its name, doesn't
directly mail anything; it just sends the change to Gerrit:


```
$ git codereview mail
```

Gerrit assigns your change a number and URL, which `git codereview mail` will
print, something like:


```
remote: New Changes:
remote:   https://review.gerrithub.io/99999 math: improved Sin, Cos and Tan precision for very large arguments
```

If you get an error instead, see the ["Troubleshooting mail
errors"](#troubleshotting-mail-errors).


### Step 5: Revise changes after a review

CUE maintainers will review your code on Gerrit, and you will get notifications
via e-mail.  You can see the review on Gerrit and comment on them there.  You
can also reply [using
e-mail](https://gerrit-review.googlesource.com/Documentation/intro-user.html#reply-by-email)
if you prefer.


If you need to revise your change after the review, edit the files in the same
branch you previously created, add them to the Git staging area, and then amend
the commit with `git codereview change`:


```
$ git codereview change -s     # amend current commit
(open $EDITOR)
$ git codereview mail          # send new changes to Gerrit
```

If you don't need to change the commit description, just save and exit from the
editor.  Remember not to touch the special `Change-Id` line.


Again, make sure that you always keep a single commit in each branch.  If you
add more commits by mistake, you can use `git rebase` to [squash them
together](https://medium.com/@slamflipstrom/a-beginners-guide-to-squashing-commits-with-git-rebase-8185cf6e62ec)
into a single one.


### CL approved!

With the review cycle complete, the CI checks green and your CL approved with
`+2`, it will be submitted. Congratulations! You will have made your first
contribution to the CUE project.


## Good commit messages

Commit messages in CUE follow a specific set of conventions, which we discuss in
this section.


Here is an example of a good one:


```
cue/ast/astutil: fix resolution bugs

This fixes several bugs and documentation bugs in
identifier resolution.

1. Resolution in comprehensions would resolve identifiers
to themselves.

2. Label aliases now no longer bind to references outside
the scope of the field. The compiler would catch this invalid
bind and report an error, but it is better not to bind in the
first place.

3. Remove some more mentions of Template labels.

4. Documentation for comprehensions was incorrect
(Scope and Node were reversed).

5. Aliases X in `X=[string]: foo` should only be visible
in foo.

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

The special notation "Fixes #12345" associates the change with issue 12345 in
the [CUE issue tracker](https://cuelang.org/issue/12345) When this change is
eventually applied, the issue tracker will automatically mark the issue as
fixed.


If the change is a partial step towards the resolution of the issue, uses the
notation "Updates #12345".  This will leave a comment in the issue linking back
to the change in Gerrit, but it will not close the issue when the change is
applied.


If you are sending a change against a subrepository, you must use the
fully-qualified syntax supported by GitHub to make sure the change is linked to
the issue in the main repository, not the subrepository.  All issues are tracked
in the main repository's issue tracker.  The correct form is "Fixes #159".



## The review process

This section explains the review process in detail and how to approach reviews
after a change has been sent to either GerritHub or GitHub.



### Common mistakes

When a change is sent to Gerrit, it is usually triaged within a few days.  A
maintainer will have a look and provide some initial review that for first-time
contributors usually focuses on basic cosmetics and common mistakes.  These
include things like:


- Commit message not following the suggested format.
- The lack of a linked GitHub issue.  The vast majority of changes require a
  linked issue that describes the bug or the feature that the change fixes or
implements, and consensus should have been reached on the tracker before
proceeding with it.  Gerrit reviews do not discuss the merit of the change, just
its implementation.  Only trivial or cosmetic changes will be accepted without
an associated issue.

### Continuous Integration (CI) checks

After an initial reading of your change, maintainers will trigger CI checks,
that run a  full test suite and [`unity`](https://github.com/cue-unity/unity)
checks.  Most CI tests complete in a few minutes, at which point a link will be
posted in Gerrit where you can see the results, or if you are submitting a PR
results are presented as checks towards the bottom of the PR.


If any of the CI checks fail, follow the link and check the full logs.  Try to
understand what broke, update your change to fix it, and upload again.
Maintainers will trigger a new CI run to see if the problem was fixed.


### Reviews

The CUE community values very thorough reviews.  Think of each review comment
like a ticket: you are expected to somehow "close" it by acting on it, either by
implementing the suggestion or convincing the reviewer otherwise.


After you update the change, go through the review comments and make sure to
reply to every one.  In GerritHub you can click the "Done" button to reply
indicating that you've implemented the reviewer's suggestion and in GitHub you
can mark a comment as resolved; otherwise, click on "Reply" and explain why you
have not, or what you have done instead.


It is perfectly normal for changes to go through several round of reviews, with
one or more reviewers making new comments every time and then waiting for an
updated change before reviewing again.  This cycle happens even for experienced
contributors, so don't be discouraged by it.


### Voting conventions in GerritHub

As they near a decision, reviewers will make a "vote" on your change.
The Gerrit voting system involves an integer in the range -2 to +2:


- **+2** The change is approved for being merged.  Only CUE maintainers can cast
  a +2 vote.
- **+1** The change looks good, but either the reviewer is requesting minor
  changes before approving it, or they are not a maintainer and cannot approve
it, but would like to encourage an approval.
- **-1** The change is not good the way it is but might be fixable.  A -1 vote
  will always have a comment explaining why the change is unacceptable.
- **-2** The change is blocked by a maintainer and cannot be approved.  Again,
  there will be a comment explaining the decision.

### Reviewed changed in GitHub

When reviewing a PR, a reviewer will indicate the nature of their response:

* **Comments** - general feedback without explicit approval.
* **Approve** - feedback and approval for this PR to accepted and submitted in
  GerritHub.
* **Request changes** - feedback that must be addressed before this PR can
  proceed.



### Submitting an approved change

After the code has been `+2`'ed in GerritHub or "Approved" in GitHub, an
approver will apply it to the `master` branch using the Gerrit user interface.
This is called "submitting the change".


The two steps (approving and submitting) are separate because in some cases
maintainers may want to approve it but not to submit it right away (for
instance, the tree could be temporarily frozen).


Submitting a change checks it into the repository.  The change description will
include a link to the code review, which will be updated with a link to the
change in the repository.  Since the method used to integrate the changes is
Git's "Cherry Pick", the commit hashes in the repository will be changed by the
submit operation.


If your change has been approved for a few days without being submitted, feel
free to write a comment in GerritHub or GitHub requesting submission.


## Miscellaneous topics

This section collects a number of other comments that are outside the
issue/edit/code review/submit process itself.



### Copyright headers

Files in the CUE repository don't list author names, both to avoid clutter and
to avoid having to keep the lists up to date.  Instead, your name will appear in
the [change log](https://review.gerrithub.io/plugins/gitiles/cue-lang/cue/+log)
and in the [`CONTRIBUTORS`](../CONTRIBUTORS) file and perhaps the
[`AUTHORS`](../AUTHORS) file.  These files are automatically generated from the
commit logs periodically.  The [`AUTHORS`](../AUTHORS) file defines who
&ldquo;The CUE Authors&rdquo;&mdash;the copyright holders&mdash;are.


New files that you contribute should use the standard copyright header:


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

(Use the current year if you're reading this in 2019 or beyond.)
Files in the repository are copyrighted the year they are added.
Do not update the copyright year on files that you change.

### Troubleshooting GerritHub mail errors

The most common way that the `git` `codereview` `mail` command fails is because
the e-mail address in the commit does not match the one that you used during the
registration process.

If you see something like...


```
remote: Processing changes: refs: 1, done
remote:
remote: ERROR:  In commit ab13517fa29487dcf8b0d48916c51639426c5ee9
remote: ERROR:  author email address XXXXXXXXXXXXXXXXXXX
remote: ERROR:  does not match your user account.
```

you need to configure Git for this repository to use the e-mail address that you
registered with.  To change the e-mail address to ensure this doesn't happen
again, run:


```
$ git config user.email email@address.com
```

Then change the commit to use this alternative e-mail address with this command:


```
$ git commit --amend --author="Author Name &lt;email@address.com&gt;"
```

Then retry by running:


```
$ git codereview mail
```


### Quickly testing your changes

Running `go test ./...` for every single change to the code tree is burdensome.
Even though it is strongly suggested to run it before sending a change, during
the normal development cycle you may want to compile and test only the package
you are developing.


In this section, we'll call the directory into which you cloned the CUE
repository `$CUEDIR`.  As CUE uses Go modules, The `cue` tool built by `go
install` will be installed in the `bin/go` in your home directory by default.

If you're changing the CUE APIs or code, you can test the results in just
this package directory.

```
$ cd $CUEDIR/cue
$ [make changes...]
$ go test
```

You don't need to build a new cue tool to test it.
Instead you can run the tests from the root.

```
$ cd $CUEDIR
$ go test ./...
```

To use the new tool you would still need to build and install it.


### Specifying a reviewer / CCing others in GerritHub

You can specify a reviewer or CC interested parties using the `-r` or `-cc`
options.  Both accept a comma-separated list of e-mail addresses:


```
$ git codereview mail -r joe@cuelang.org -cc mabel@example.com,math-nuts@swtch.com
```


### Synchronize your client with GerritHub

While you were working, others might have submitted changes to the repository.
To update your local branch, run


```
$ git codereview sync
```

(Under the covers this runs
`git` `pull` `-r`.)



### Reviewing code by others

As part of the review process reviewers can propose changes directly (in the
GitHub workflow this would be someone else attaching commits to a pull request).

You can import these changes proposed by someone else into your local Git
repository.  On the Gerrit review page, click the "Download ▼" link in the upper
right corner, copy the "Checkout" command and run it from your local Git repo.
It will look something like this:


```
$ git fetch https://review.gerrithub.io/a/cue-lang/cue refs/changes/67/519567/1 && git checkout FETCH_HEAD
```

To revert, change back to the branch you were working in.


### Set up git aliases

The `git-codereview` command can be run directly from the shell
by typing, for instance,


```
$ git codereview sync
```

but it is more convenient to set up aliases for `git-codereview`'s own
subcommands, so that the above becomes,


```
$ git sync
```

The `git-codereview` subcommands have been chosen to be distinct from Git's own,
so it's safe to define these aliases.  To install them, copy this text into your
Git configuration file (usually `.gitconfig` in your home directory):


```
[alias]
	change = codereview change
	gofmt = codereview gofmt
	mail = codereview mail
	pending = codereview pending
	submit = codereview submit
	sync = codereview sync
```


### Sending multiple dependent changes

Advanced users may want to stack up related commits in a single branch.  Gerrit
allows for changes to be dependent on each other, forming such a dependency
chain.  Each change will need to be approved and submitted separately but the
dependency will be visible to reviewers.


To send out a group of dependent changes, keep each change as a different commit
under the same branch, and then run:


```
$ git codereview mail HEAD
```

Make sure to explicitly specify `HEAD`, which is usually not required when
sending single changes.

This is covered in more detail in [the Gerrit
documentation](https://gerrit-review.googlesource.com/Documentation/concept-changes.html).

### Do I really have to add the `-s` flag to each commit?

Earlier in this guide we explained the role the [Developer Certificate of
Origin](https://developercertificate.org/) plays in contributions to the CUE
project. we also explained how `git commit -s` can be used to sign-off each
commit. But:

* it's easy to forget the `-s` flag;
* it's not always possible/easy to fix up other tools that wrap the `git commit`
  step.

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
handling issues can be found in the [Code of
Conduct](https://cuelang.org/docs/contribution_guidelines/conduct).
