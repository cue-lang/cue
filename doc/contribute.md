# Contribution Guide


The CUE project welcomes all contributors.

This document is a guide to help you through the process
of contributing to the CUE project, which is a little different
from that used by other open source projects.
We assume you have a basic understanding of Git and Go.


## Becoming a contributor

### Overview

The first step is registering as a CUE contributor and configuring your environment.
Here is a checklist of the required steps to follow:

- **Step 0**: Decide which email address you want to use for contributions.
- **Step 1**: Read and prepare for asserting a Developer Certificate of Origin.
- **Step 2**: Set up a [GerritHub](http://gerrithub.io/) account.
- **Step 3**: Install `git-codereview` by running: `GO111MODULE=on go get
  golang.org/x/review/git-codereview` (or just `go install
  golang.org/x/review/git-codereview@latest` if you are using Go 1.16)

Note that if you plan to contribute via GitHub, only Steps 0 and 1 are required.

The rest of this chapter elaborates on these instructions.  If you have
completed the steps above (either manually or through the tool), jump to "Before
contributing code."

### Step 0: Decide which email address you want to use for contributions

A contribution to CUE is made through a specific e-mail address.  Make sure to
use the same account throughout the process and for all your subsequent
contributions.  You may need to decide whether to use a personal address or a
corporate address.  The choice will depend on who will own the copyright for the
code that you will be writing and submitting.  You might want to discuss this
topic with your employer before deciding which account to use.

You also need to make sure that your Git tool is configured to create commits
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

If the copyright holder for the code you are submitting changes&mdash;for
example, if you start contributing code on behalf of a new company&mdash;please
send mail to the [CUE Owners](mailto:owners@cuelang.org).  This will let us know
the situation so we can make sure an appropriate agreement is completed and
update the `AUTHORS` file.

### Step 1: Read and prepare for asserting a Developer Certificate of Origin

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
that matches the commit author. When commiting, use the `--signoff` flag:

```
git commit -s
```

### Step 2: Setup a GerritHub account

If you have not used GerritHub before, setting up an account is a simple
process:

- Visit [GerritHub](http://gerrithub.io/).
- Click "First Time Sign In".
- Clickt the green "Sign In" button, to sign in using your GitHub
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
other [crendential helper](https://git-scm.com/docs/gitcredentials). If you have
any troubles with this step, please [raise an
issue](https://cuelang.org/issues/new).

Then visit https://review.gerrithub.io/admin/repos/cue-lang/cue, then click
"HTTP" or "SSH" then copy the corresponding "Clone" command.


### Step 3: Install the git-codereview command

Changes to CUE must be reviewed before they are accepted, no matter who makes
the change.  A custom `git` command called `git-codereview` simplifies sending
changes to Gerrit.

<!-- Keep the following in sync with cmd/cue/cmd/testdata/script/contributing_install*.txt -->

Install the `git-codereview` command by running,

```
$ GO111MODULE=on go get golang.org/x/review/git-codereview
```

or with Go 1.16:

```
$ go install golang.org/x/review/git-codereview@latest
```

Make sure `git-codereview` is installed in your shell path, so that the
`git` command can find it.
Check that


```
$ git codereview help
```

prints help text, not an error.


On Windows, when using git-bash you must make sure that
`git-codereview.exe` is in your `git` exec-path.
Run `git --exec-path` to discover the right location then create a
symbolic link or just copy the executable from $GOPATH/bin to this directory.


## Before contributing code

<!--
TODO
The project welcomes code patches, but to make sure things are well
coordinated you should discuss any significant change before starting
the work.
It's recommended that you signal your intention to contribute in the
issue tracker, either by <a href="https://cuelang.org/issue/new">filing
a new issue</a> or by claiming
an <a href="https://cuelang.org/issues">existing one</a>.

-->

### Check the issue tracker

Whether you already know what contribution to make, or you are searching for
an idea, the [issue tracker](https://cuelang.org/issues) is
always the first place to go.
Issues are triaged to categorize them and manage the workflow.


Most issues will be marked with one of the following workflow labels:


-	**NeedsInvestigation**: The issue is not fully understood
	and requires analysis to understand the root cause.
-	**NeedsDecision**: the issue is relatively well understood, but the
	CUE team hasn't yet decided the best way to address it.
	It would be better to wait for a decision before writing code.
	If you are interested on working on an issue in this state,
	feel free to "ping" maintainers in the issue's comments
	if some time has passed without a decision.
-	**NeedsFix**: the issue is fully understood and code can be written
	to fix it.

You can use GitHub's search functionality to find issues to help out with. Examples:


-	Issues that need investigation:
	[`is:issue is:open label:NeedsInvestigation`](
		https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsInvestigation)
-	Issues that need a fix:
	[`is:issue is:open label:NeedsFix`](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsFix)
-	Issues that need a fix and have a CL:
    [`is:issue is:open label:NeedsFix "cuelang.org/cl"`](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsFix+%22golang.org%2Fcl%22)
-	Issues that need a fix and do not have a CL:
    [`is:issue is:open label:NeedsFix NOT "cuelang.org/cl"`](https://cuelang.org/issues?q=is%3Aissue+is%3Aopen+label%3ANeedsFix+NOT+%22golang.org%2Fcl%22)

### Open an issue for any new problem

Excluding very trivial changes, all contributions should be connected
to an existing issue.
Feel free to open one and discuss your plans.
This process gives everyone a chance to validate the design,
helps prevent duplication of effort,
and ensures that the idea fits inside the goals for the language and tools.
It also checks that the design is sound before code is written;
the code review tool is not the place for high-level discussions.


<!--
TODO
When planning work, please note that the CUE project follows a <a
href="https://cuelang.org/wiki/CUE-Release-Cycle">six-month development cycle</a>.
The latter half of each cycle is a three-month feature freeze during
which only bug fixes and documentation updates are accepted.
New contributions can be sent during a feature freeze, but they will
not be merged until the freeze is over.


Significant changes to the language, libraries, or tools must go
through the
<a href="https://cuelang.org/s/proposal-process">change proposal process</a>
before they can be accepted.


Sensitive security-related issues (only!) should be reported to <a href="mailto:security@cuelang.org">security@cuelang.org</a>.


## Sending a change via GitHub

First-time contributors that are already familiar with the
<a href="https://guides.github.com/introduction/flow/">GitHub flow</a>
are encouraged to use the same process for CUE contributions.
Even though CUE
maintainers use Gerrit for code review, a bot called Gopherbot has been created to sync
GitHub pull requests to Gerrit.


Open a pull request as you normally would.
Gopherbot will create a corresponding Gerrit change and post a link to
it on your GitHub pull request; updates to the pull request will also
get reflected in the Gerrit change.
When somebody comments on the change, their comment will be also
posted in your pull request, so you will get a notification.


Some things to keep in mind:


<ul>
<li>
To update the pull request with new code, just push it to the branch; you can either
add more commits, or rebase and force-push (both styles are accepted).
</li>
<li>
If the request is accepted, all commits will be squashed, and the final
commit description will be composed by concatenating the pull request's
title and description.
The individual commits' descriptions will be discarded.
See Writing good commit messages</a> for some
suggestions.
</li>
<li>
Gopherbot is unable to sync line-by-line codereview into GitHub: only the
contents of the overall comment on the request will be synced.
Remember you can always visit Gerrit to see the fine-grained review.
</li>
</ul>
-->

## Sending a change via Gerrit

It is not possible to fully sync Gerrit and GitHub,
although things are improving,
so we recommend learning Gerrit.
It's different but powerful and familiarity
with help you understand the flow.


### Overview

This is an overview of the overall process:


- **Step 1:** Clone the CUE source code from GerritHub. Visit
https://review.gerrithub.io/admin/repos/cue-lang/cue, then click
"SSH" or "HTTP" dependending on which authentication mechanism you configured
above. Then copy and run the corresponding "Clone" command.

Then make sure it's stable by compiling and testing it once:

```
$ cd cue
$ go test ./...
$ go install ./cmd/cue
```

- **Step 2:** Prepare changes in a new branch, created from the master branch.
To commit the changes, use `git` `codereview` `change`; that
will create or amend a single commit in the branch.
```
$ git checkout -b mybranch
$ [edit files...]
$ git add [files...]
$ git codereview change   # create commit in the branch
$ [edit again...]
$ git add [files...]
$ git codereview change   # amend the existing commit with new changes
$ [etc.]
```

- **Step 3:** Test your changes, re-running `go test`.
```
$ go test ./...    # recompile and test
```

- **Step 4:** Send the changes for review to Gerrit using `git`
`codereview` `mail` (which doesn't use e-mail, despite the name).
```
$ git codereview mail     # send changes to Gerrit
```

- **Step 5:** After a review, apply changes to the same single commit
and mail them to Gerrit again:
```
$ [edit files...]
$ git add [files...]
$ git codereview change   # update same commit
$ git codereview mail     # send to Gerrit again
```

The rest of this section describes these steps in more detail.



### Step 1: Clone the CUE source code

In addition to a recent CUE installation, you need to have a local copy of the source
checked out from the correct repository.
You can check out the CUE source repo onto your local file system anywhere
you want as long as it's outside your `GOPATH`.

Visit https://review.gerrithub.io/admin/repos/cue-lang/cue, then click
"SSH" or "HTTP" dependending on which authentication mechanism you configured
above. Then copy and run the corresponding "Clone" command.

Then make sure it's stable by compiling and testing it once:

```
$ cd cue
$ go test ./...
# go install ./cmd/cue
```

### Step 2: Prepare changes in a new branch

Each CUE change must be made in a separate branch, created from the master branch.
You can use
the normal `git` commands to create a branch and add changes to the
staging area:


```
$ git checkout -b mybranch
$ [edit files...]
$ git add [files...]
```

To commit changes, instead of `git commit`, use `git codereview change`.


```
$ git codereview change
(open $EDITOR)
```

You can edit the commit description in your favorite editor as usual.
The  `git` `codereview` `change` command
will automatically add a unique Change-Id line near the bottom.
That line is used by Gerrit to match successive uploads of the same change.
Do not edit or delete it.
A Change-Id looks like this:


```
Change-Id: I2fbdbffb3aab626c4b6f56348861b7909e3e8990
```

The tool also checks that you've
run `go` `fmt` over the source code, and that
the commit message follows the suggested format.


If you need to edit the files again, you can stage the new changes and
re-run `git` `codereview` `change`: each subsequent
run will amend the existing commit while preserving the Change-Id.


Make sure that you always keep a single commit in each branch.
If you add more
commits by mistake, you can use `git` `rebase` to
[squash them together](https://stackoverflow.com/questions/31668794/squash-all-your-commits-in-one-before-a-pull-request-in-github)
into a single one.



### Step 3: Test your changes

You've written and tested your code, but
before sending code out for review, run <i>all the tests for the whole
tree</i> to make sure the changes don't break other packages or programs:


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

Gerrit assigns your change a number and URL, which `git` `codereview` `mail` will print, something like:


```
remote: New Changes:
remote:   https://review.gerrithub.io/99999 math: improved Sin, Cos and Tan precision for very large arguments
```

If you get an error instead, check the
Troubleshooting mail errors section.


If your change relates to an open GitHub issue and you have followed the
suggested commit message format, the issue will be updated in a few minutes by a bot,
linking your Gerrit change to it in the comments.



### Step 5: Revise changes after a review

CUE maintainers will review your code on Gerrit, and you will get notifications via e-mail.
You can see the review on Gerrit and comment on them there.
You can also reply
[using e-mail](https://gerrit-review.googlesource.com/Documentation/intro-user.html#reply-by-email)
if you prefer.


If you need to revise your change after the review, edit the files in
the same branch you previously created, add them to the Git staging
area, and then amend the commit with
`git` `codereview` `change`:


```
$ git codereview change     # amend current commit
(open $EDITOR)
$ git codereview mail       # send new changes to Gerrit
```

If you don't need to change the commit description, just save and exit from the editor.
Remember not to touch the special Change-Id line.


Again, make sure that you always keep a single commit in each branch.
If you add more
commits by mistake, you can use `git rebase` to
[squash them together](https://stackoverflow.com/questions/31668794/squash-all-your-commits-in-one-before-a-pull-request-in-github)
into a single one.


## Good commit messages

Commit messages in CUE follow a specific set of conventions,
which we discuss in this section.


Here is an example of a good one:


```
math: improve Sin, Cos and Tan precision for very large arguments

The existing implementation has poor numerical properties for
large arguments, so use the McGillicutty algorithm to improve
accuracy above 1e10.

The algorithm is described at https://wikipedia.org/wiki/McGillicutty_Algorithm

Fixes #159
```

### First line

The first line of the change description is conventionally a short one-line
summary of the change, prefixed by the primary affected package.


A rule of thumb is that it should be written so to complete the sentence
"This change modifies CUE to _____."
That means it does not start with a capital letter, is not a complete sentence,
and actually summarizes the result of the change.


Follow the first line by a blank line.


### Main content

The rest of the description elaborates and should provide context for the
change and explain what it does.
Write in complete sentences with correct punctuation, just like
for your comments in CUE.
Don't use HTML, Markdown, or any other markup language.



### Referencing issues

The special notation "Fixes #12345" associates the change with issue 12345 in the
[CUE issue tracker](https://cuelang.org/issue/12345)
When this change is eventually applied, the issue
tracker will automatically mark the issue as fixed.


If the change is a partial step towards the resolution of the issue,
uses the notation "Updates #12345".
This will leave a comment in the issue
linking back to the change in Gerrit, but it will not close the issue
when the change is applied.


If you are sending a change against a subrepository, you must use
the fully-qualified syntax supported by GitHub to make sure the change is
linked to the issue in the main repository, not the subrepository.
All issues are tracked in the main repository's issue tracker.
The correct form is "Fixes #159".



## The review process

This section explains the review process in detail and how to approach
reviews after a change has been mailed.



### Common beginner mistakes

When a change is sent to Gerrit, it is usually triaged within a few days.
A maintainer will have a look and provide some initial review that for first-time
contributors usually focuses on basic cosmetics and common mistakes.
These include things like:


- Commit message not following the suggested
format.
- The lack of a linked GitHub issue.
The vast majority of changes
require a linked issue that describes the bug or the feature that the change
fixes or implements, and consensus should have been reached on the tracker
before proceeding with it.
Gerrit reviews do not discuss the merit of the change,
just its implementation.
Only trivial or cosmetic changes will be accepted without an associated issue.

<!-- TODO
<li>
Change sent during the freeze phase of the development cycle, when the tree
is closed for general changes.
In this case,
a maintainer might review the code with a line such as `R=cue1.1`,
which means that it will be reviewed later when the tree opens for a new
development window.
You can add `R=cue1.XX` as a comment yourself
if you know that it's not the correct time frame for the change.
</li>
-->

<!--
TODO
### Trybots

After an initial reading of your change, maintainers will trigger trybots,
a cluster of servers that will run the full test suite on several different
architectures.
Most trybots complete in a few minutes, at which point a link will
be posted in Gerrit where you can see the results.


If the trybot run fails, follow the link and check the full logs of the
platforms on which the tests failed.
Try to understand what broke, update your patch to fix it, and upload again.
Maintainers will trigger a new trybot run to see
if the problem was fixed.


Sometimes, the tree can be broken on some platforms for a few hours; if
the failure reported by the trybot doesn't seem related to your patch, go to the
<a href="https://build.cuelang.org">Build Dashboard</a> and check if the same
failure appears in other recent commits on the same platform.
In this case,
feel free to write a comment in Gerrit to mention that the failure is
unrelated to your change, to help maintainers understand the situation.

-->

### Reviews

The CUE community values very thorough reviews.
Think of each review comment like a ticket: you are expected to somehow "close" it
by acting on it, either by implementing the suggestion or convincing the
reviewer otherwise.


After you update the change, go through the review comments and make sure
to reply to every one.
You can click the "Done" button to reply
indicating that you've implemented the reviewer's suggestion; otherwise,
click on "Reply" and explain why you have not, or what you have done instead.


It is perfectly normal for changes to go through several round of reviews,
with one or more reviewers making new comments every time
and then waiting for an updated change before reviewing again.
This cycle happens even for experienced contributors, so
don't be discouraged by it.


### Voting conventions

As they near a decision, reviewers will make a "vote" on your change.
The Gerrit voting system involves an integer in the range -2 to +2:


-	**+2** The change is approved for being merged.
	Only CUE maintainers can cast a +2 vote.
-	**+1** The change looks good, but either the reviewer is requesting
	minor changes before approving it, or they are not a maintainer and cannot
	approve it, but would like to encourage an approval.
-	**-1** The change is not good the way it is but might be fixable.
	A -1 vote will always have a comment explaining why the change is unacceptable.
-	**-2** The change is blocked by a maintainer and cannot be approved.
	Again, there will be a comment explaining the decision.

### Submitting an approved change

After the code has been +2'ed, an approver will
apply it to the master branch using the Gerrit user interface.
This is called "submitting the change".


The two steps (approving and submitting) are separate because in some cases maintainers
may want to approve it but not to submit it right away (for instance,
the tree could be temporarily frozen).


Submitting a change checks it into the repository.
The change description will include a link to the code review,
which will be updated with a link to the change
in the repository.
Since the method used to integrate the changes is Git's "Cherry Pick",
the commit hashes in the repository will be changed by
the submit operation.


If your change has been approved for a few days without being
submitted, feel free to write a comment in Gerrit requesting
submission.



<!--

### More information

TODO
In addition to the information here, the CUE community maintains a <a
href="https://cuelang.org/wiki/CodeReview">CodeReview</a> wiki page.
Feel free to contribute to this page as you learn more about the review process.

-->


## Miscellaneous topics

This section collects a number of other comments that are
outside the issue/edit/code review/submit process itself.



### Copyright headers

Files in the CUE repository don't list author names, both to avoid clutter
and to avoid having to keep the lists up to date.
Instead, your name will appear in the
[change log](https://review.gerrithub.io/plugins/gitiles/cue-lang/cue/+log) and in the
[`CONTRIBUTORS`](../CONTRIBUTORS) file and perhaps the
[`AUTHORS`](../AUTHORS) file.
These files are automatically generated from the commit logs periodically.
The [`AUTHORS`](../AUTHORS) file defines who &ldquo;The CUE
Authors&rdquo;&mdash;the copyright holders&mdash;are.


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





### Troubleshooting mail errors

The most common way that the `git` `codereview` `mail`
command fails is because the e-mail address in the commit does not match the one
that you used during the registration process.

If you see something like...


```
remote: Processing changes: refs: 1, done
remote:
remote: ERROR:  In commit ab13517fa29487dcf8b0d48916c51639426c5ee9
remote: ERROR:  author email address XXXXXXXXXXXXXXXXXXX
remote: ERROR:  does not match your user account.
```

you need to configure Git for this repository to use the
e-mail address that you registered with.
To change the e-mail address to ensure this doesn't happen again, run:


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

Running `go test ./...` for every single change to the code tree
is burdensome.
Even though it is strongly suggested to run it before
sending a change, during the normal development cycle you may want
to compile and test only the package you are developing.


In this section, we'll call the directory into which you cloned the CUE repository `$CUEDIR`.
As CUE uses Go modules, The `cue` tool built by
`go install` will be installed in the `bin/go` in your
home directory by default.

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


<!--
TODO
### Contributing to subrepositories (cuelang.org/x/...)

If you are contributing a change to a subrepository, obtain the
CUE package using `go get`.
For example, to contribute
to `cuelang.org/x/editor/vscode`, check out the code by running:


```
$ go get -d cuelang.org/editor/vscode
```

Then, change your directory to the package's source directory
(`$GOPATH/src/cuelang.org/x/oauth2`), and follow the
normal contribution flow.

-->

### Specifying a reviewer / CCing others

<!--
TODO:

Unless explicitly told otherwise, such as in the discussion leading
up to sending in the change, it's better not to specify a reviewer.
All changes are automatically CC'ed to the
<a href="https://groups.google.com/group/cue-codereviews">cue-codereviews@googlegroups.com</a>
mailing list.
If this is your first ever change, there may be a moderation
delay before it appears on the mailing list, to prevent spam.

-->

You can specify a reviewer or CC interested parties
using the `-r` or `-cc` options.
Both accept a comma-separated list of e-mail addresses:


```
$ git codereview mail -r joe@cuelang.org -cc mabel@example.com,math-nuts@swtch.com
```


### Synchronize your client

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

You can import these changes proposed by someone else into your local Git repository.
On the Gerrit review page, click the "Download ▼" link in the upper right
corner, copy the "Checkout" command and run it from your local Git repo.
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

The `git-codereview` subcommands have been chosen to be distinct from
Git's own, so it's safe to define these aliases.
To install them, copy this text into your
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

Advanced users may want to stack up related commits in a single branch.
Gerrit allows for changes to be dependent on each other, forming such a dependency chain.
Each change will need to be approved and submitted separately but the dependency
will be visible to reviewers.


To send out a group of dependent changes, keep each change as a different commit under
the same branch, and then run:


```
$ git codereview mail HEAD
```

Make sure to explicitly specify `HEAD`, which is usually not required when sending
single changes.

