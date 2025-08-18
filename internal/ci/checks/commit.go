// Copyright 2024 CUE Authors
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

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	mdast "github.com/yuin/goldmark/ast"
	mdextension "github.com/yuin/goldmark/extension"
	mdtext "github.com/yuin/goldmark/text"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := checkCommit(wd); err != nil {
		log.Fatal(err)
	}
}

func checkCommit(dir string) error {
	body, err := runCmd(dir, "git", "log", "-1", "--format=%B", "HEAD")
	if err != nil {
		return err
	}

	// Ensure that commit messages have a blank second line.
	// We know that a commit message must be longer than a single
	// line because each commit must be signed-off.
	lines := strings.Split(body, "\n")
	if len(lines) > 1 && lines[1] != "" {
		return fmt.Errorf("the second line of a commit message must be blank")
	}

	// All authors, including co-authors, must have a signed-off trailer by email.
	// Note that trailers are in the form "Name <email>", so grab the email with regexp.
	// For now, we require the sorted lists of author and signer emails to match.
	// Note that this also fails if a commit isn't signed-off at all.
	//
	// In Gerrit we already enable a form of this via https://gerrit-review.googlesource.com/Documentation/project-configuration.html#require-signed-off-by,
	// but it does not support co-authors nor can it be used when testing GitHub PRs.
	authorEmail, err := runCmd(dir, "git", "log", "-1", "--format=%ae")
	if err != nil {
		return err
	}
	coauthorList, err := runCmd(dir, "git", "log", "-1", "--format=%(trailers:key=Co-authored-by,valueonly)")
	if err != nil {
		return err
	}
	authors := slices.Concat([]string{authorEmail}, extractEmails(coauthorList))
	slices.Sort(authors)
	authors = slices.Compact(authors)

	signerList, err := runCmd(dir, "git", "log", "-1", "--format=%(trailers:key=Signed-off-by,valueonly)")
	if err != nil {
		return err
	}
	signers := extractEmails(signerList)
	slices.Sort(signers)
	signers = slices.Compact(signers)

	if !slices.Equal(authors, signers) {
		return fmt.Errorf("commit author email addresses %q do not match signed-off-by trailers %q",
			authors, signers)
	}

	// Forbid @-mentioning any GitHub usernames in commit messages,
	// as that will lead to notifications which are likely unintended.
	// If one must include a similar-looking snippet, like @embed(),
	// they can be placed inside Markdown's single-line single-backticks
	// without notifications being generated (e.g. "This uses `@embed()` to ...").
	//
	// Note that we parse the body as markdown including git trailers, but that's okay.
	// Note that GitHub does not interpret mentions in titles, but we still check them
	// for the sake of being conservative and consistent.
	//
	// TODO: this doesn't catch mentions in block quotes, which cause notifications.
	// cf. https://cuelang.org/issue/4026
	md := goldmark.New(
		goldmark.WithExtensions(mdextension.GFM),
	)
	docBody := []byte(body)
	doc := md.Parser().Parse(mdtext.NewReader(docBody))
	if err := mdast.Walk(doc, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if !entering {
			return mdast.WalkContinue, nil
		}
		// Uncomment for some quick debugging.
		// fmt.Printf("%T\n%q\n\n", node, node.Text(docBody))
		switch node.(type) {
		case *mdast.CodeSpan:
			return mdast.WalkSkipChildren, nil
		case *mdast.Text:
			text := node.Text(docBody)
			if m := rxUserMention.FindSubmatch(text); m != nil {
				return mdast.WalkStop, fmt.Errorf("commit mentions GitHub user %q; enclose code in single backticks", m[2])
			}
		}
		return mdast.WalkContinue, nil
	}); err != nil {
		return err
	}
	return nil
}

func runCmd(dir string, exe string, args ...string) (string, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(bytes.TrimSpace(out)), err
}

var (
	rxExtractEmail = regexp.MustCompile(`.*<(.*)\>$`)
	rxUserMention  = regexp.MustCompile(`(^|\s)(@[a-z0-9][a-z0-9-]*)`)
)

func extractEmails(list string) []string {
	lines := strings.Split(list, "\n")
	var emails []string
	for _, line := range lines {
		m := rxExtractEmail.FindStringSubmatch(line)
		if m == nil {
			continue // no match; discard this line
		}
		emails = append(emails, m[1])
	}
	return emails
}
