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
//     import "github.com/cue-lang/tmp/internal/ci/base"
//
//     // Create an instance of base
//     _base: base & core { params: {
//         // any values you need to set that are outside of core
//         ...
//     }}
//
package base

import (
	"strings"
)

// Package parameters
githubRepositoryPath: *(URLPath & {#url: githubRepositoryURL, _}) | string
githubRepositoryURL:    *("https://github.com/" + githubRepositoryPath) | string
gerritHubHostname:      *"review.gerrithub.io" | string
gerritHubRepositoryURL: *("https://\(gerritHubHostname)/a/" + githubRepositoryPath) | string
trybotRepositoryPath:   *(githubRepositoryPath + "-" + trybot.key) | string
trybotRepositoryURL:    *("https://github.com/" + trybotRepositoryPath) | string

defaultBranch:     *"master" | string
testDefaultBranch: *"ci/test" | _
protectedBranchPatterns: *[defaultBranch] | [...string]
releaseTagPrefix:  *"v" | string
releaseTagPattern: *(releaseTagPrefix + "*") | string

botGitHubUser:                      string
botGitHubUserTokenSecretsKey:       *(strings.ToUpper(botGitHubUser) + "_GITHUB_PAT") | string
botGitHubUserEmail:                 string
botGerritHubUser:                   *botGitHubUser | string
botGerritHubUserPasswordSecretsKey: *(strings.ToUpper(botGitHubUser) + "_GERRITHUB_PASSWORD") | string
botGerritHubUserEmail:              *botGitHubUserEmail | string

unprivilegedBotGitHubUser:                               "not" + botGitHubUser
unprivilegedBotGitHubUserCentralRegistryTokenSecretsKey: *(strings.ToUpper(unprivilegedBotGitHubUser) + "_CUE_TOKEN") | string

// Most repositories will already be a Go module, so they can use a tool dependency.
// Others can override this string with e.g. `cue` from the `setup-cue` action.
cueCommand: *"go tool cue" | string

workflowFileExtension: ".yaml"

// Linux is used by lots of repositories for many workflows,
// so each repository can decide which jobs really need larger machines.
// Note that using a larger machine with more CPUs and memory lowers
// the amount of jobs that can be run concurrently at once.
//
// At the time of writing, "small" is 4 CPUs and 8GiB of memory
// and "large" is 8 CPUs and 16GiB of memory.
// With our concurrency limit for Linux at 64 CPUs and 128 GiB,
// using "small" rather than "large" allows 16 rather than 8 jobs at once.
//
// TODO(mvdan): use aliases again once they can work with the "overrides.cache-tag" suffix below.
// linuxSmallMachine: "ns-linux-amd64-small"
// linuxLargeMachine: "ns-linux-amd64-large"
linuxSmallMachine: "namespace-profile-linux-amd64"
linuxLargeMachine: "namespace-profile-linux-amd64-large"

// By default, the main "trybot" test job is run on the small machine.
// Note that cheap workflows, or those which don't keep a human waiting,
// should always use linuxSmallMachine.
linuxMachine: string | *linuxSmallMachine

// MacOS on Namespace doesn't really provide small machines,
// as the smallest is 6 CPUs and 14 GiB on M2 hardware.
// Most repos only test on Linux - it's just the cue repo now -
// so it's actually fine if we always use "large" MacOS and Windows machines.
macosMachine:   string | *"ns-macos-arm64"
windowsMachine: string | *"ns-windows-amd64"

// Append this suffix to "runs-on" to prevent workflows from sharing the default caches
// for the repository and runner profile. For example, this is useful so that
// a "tip_triggers" workflow on the cue repo to push to rebuild tip.cuelang.org
// does not share cache volumes with the main "trybot" workflow, which makes much heavier
// use of the cache. Mixing the two would lead to less effective cache updates.
overrideCacheTagDispatch: ";overrides.cache-tag=cue-dispatch-workflow"

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
latestGo: "1.25.x"
// Some repositories also want to ensure that the previous version works.
previousGo: "1.24.x"

codeReview: #codeReview & {
	github: githubRepositoryURL
	gerrit: gerritHubRepositoryURL
}

// Define some shared keys and human-readable names.
//
// trybot.key and unity.key are shared with
// github.com/cue-lang/contrib-tools/cmd/cueckoo.  The keys are used across various CUE
// workflows and their consistency in those various locations is therefore
// crucial. As such, we assert specific values for the keys here rather than
// just deriving values from the human-readable names.
//
// trybot.name is used by the trybot GitHub workflow and by gerritstatusupdater as
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
