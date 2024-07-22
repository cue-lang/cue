#!/bin/bash

# Ensure that commit messages have a blank second line.
# We know that a commit message must be longer than a single
# line because each commit must be signed-off.
if git log --format=%B -n 1 HEAD | sed -n '2{/^$/{q1}}'; then
	echo "second line of commit message must be blank"
	exit 1
fi

# All authors, including co-authors, must have a signed-off trailer by email.
# Note that trailers are in the form "Name <email>", so grab the email with sed.
# For now, we require the sorted lists of author and signer emails to match.
# Note that this also fails if a commit isn't signed-off at all.
#
# In Gerrit we already enable a form of this via https://gerrit-review.googlesource.com/Documentation/project-configuration.html#require-signed-off-by,
# but it does not support co-authors nor can it be used when testing GitHub PRs.
commit_authors="$(
	{
		git log -1 --pretty='%ae'
		git log -1 --pretty='%(trailers:key=Co-authored-by,valueonly)' | sed -ne 's/.* <\(.*\)>/\1/p'
	} | sort -u
)"
commit_signers="$(
	{
		git log -1 --pretty='%(trailers:key=Signed-off-by,valueonly)' | sed -ne 's/.* <\(.*\)>/\1/p'
	} | sort -u
)"
if [[ "${commit_authors}" != "${commit_signers}" ]]; then
	echo "Error: commit author email addresses do not match signed-off-by trailers"
	echo
	echo "Authors:"
	echo "${commit_authors}"
	echo
	echo "Signers:"
	echo "${commit_signers}"
	exit 1
fi
