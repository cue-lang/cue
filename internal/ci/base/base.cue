// Copyright 2022 The CUE Authors
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

// package base is a collection of workflows, jobs, stes etc that are common to
// CUE projects and the workflows they specify. The package itself needs to be
// instantiated to parameterise many of the exported definitions.
//
// For example a package using base would do something like this:
//
//     package workflows
//
//     import "cuelang.org/go/internal/ci/base"
//
//     // Create an instance of base
//     _base: base & core { params: {
//         // any values you need to set that are outside of core
//         ...
//     }}
//
package base

import (
	"path"
	"strings"
)

// Package parameters
#githubRepositoryURL:          string
#defaultBranch:                string
#testDefaultBranch:            "ci/test"
#botGitHubUser:                string
#botGitHubUserTokenSecretsKey: string
#protectedBranchPatterns: [...string]
#releaseTagPattern: string

#linuxMachine:                       string
#gerritHubHostname:                  string
#gerritHubRepositoryURL:             string
#trybotRepositoryURL:                *(#githubRepositoryURL + "-" + trybot.key) | string
#botGitHubUserEmail:                 string
#botGerritHubUser:                   *#botGitHubUser | string
#botGerritHubUserPasswordSecretsKey: string
#botGerritHubUserEmail:              *#botGitHubUserEmail | string

// Pending cuelang.org/issue/1433, hack around defaulting #gerritHubRepository
// based on #repository
let _#repositoryURLNoScheme = strings.Split(#githubRepositoryURL, "//")[1]
#gerritHubRepository: *("https://\(#gerritHubHostname)/a/" + path.Base(path.Dir(_#repositoryURLNoScheme)) + "/" + path.Base(_#repositoryURLNoScheme)) | _

// Define some shared keys and human-readable names.
//
// trybot.key and unity.key are shared with
// github.com/cue-sh/tools/cmd/cueckoo.  The keys are used across various CUE
// workflows and their consistency in those various locations is therefore
// crucial. As such, we assert specific values for the keys here rather than
// just deriving values from the human-readable names.
//
// trybot.name is by the trybot GitHub workflow and by gerritstatusupdater as
// an identifier in the status updates that are posted as reviews for this
// workflows, but also as the result label key, e.g.  "TryBot-Result" would be
// the result label key for the "TryBot" workflow. This name also shows up in
// the CI badge in the top-level README.
trybot: {
	key:  "trybot" & strings.ToLower(name)
	name: "TryBot"
}

unity: {
	key:  "unity" & strings.ToLower(name)
	name: "Unity"
}
