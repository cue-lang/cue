// package core contains data values that are commont to all CUE
// configurations.  This not only includes GitHub workflows, but also things
// like gerrit configuration etc.
package core

import (
	"cuelang.org/go/internal/ci/base"
)

// The machines that we use
linuxMachine:   "ubuntu-22.04"
macosMachine:   "macos-11"
windowsMachine: "windows-2022"

// Define core URLs that will be used in the codereview.cfg and GitHub workflows
githubRepositoryURL:  "https://github.com/cue-lang/cue"
gerritHubHostname:    "review.gerrithub.io"
gerritRepositoryURL:  "https://\(gerritHubHostname)/a/cue-lang/cue"
githubRepositoryPath: base.#URLPath & {#url: githubRepositoryURL, _}
unityRepositoryURL:   "https://github.com/cue-unity/unity"

botGitHubUser:                      "cueckoo"
botGitHubUserTokenSecretsKey:       "CUECKOO_GITHUB_PAT"
botGitHubUserEmail:                 "cueckoo@gmail.com"
botGerritHubUser:                   botGitHubUser
botGerritHubUserPasswordSecretsKey: "CUECKOO_GERRITHUB_PASSWORD"
botGerritHubUserEmail:              botGitHubUserEmail

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
latestStableGo: "1.19.x"

// Use a specific latest version for release builds.
// Note that we don't want ".x" for the sake of reproducibility,
// so we instead pin a specific Go release.
pinnedReleaseGo: "1.19.7"

goreleaserVersion: "v1.13.1"

defaultBranch: "master"

// releaseBranchPrefix is the git branch name prefix used to identify
// release branches.
releaseBranchPrefix: "release-branch."

// releaseBranchPattern is the GitHub pattern that corresponds to
// releaseBranchPrefix.
releaseBranchPattern: releaseBranchPrefix + "*"

// releaseTagPrefix is the prefix used to identify all git tag that correspond
// to semver releases
releaseTagPrefix: "v"

// releaseTagPattern is the GitHub glob pattern that corresponds to
// releaseTagPrefix.
releaseTagPattern: releaseTagPrefix + "*"

// zeroReleaseTagSuffix is the suffix used to identify all "zero" releases.
// When we create a release branch for v0.$X.0, it's likely that commits on the
// default branch will from that point onwards be intended for the $X+1
// version. However, unless we tag the next commit after the release branch, it
// might be the case that pseudo versions of those later commits refer to the
// $X release.
//
// A "zero" tag fixes this when applied to the first commit after a release
// branch. Critically, the -0.dev pre-release suffix is ordered before -alpha.
// tags.
zeroReleaseTagSuffix: "-0.dev"

// zeroReleaseTagPattern is the GitHub glob pattern that corresponds
// zeroReleaseTagSuffix.
zeroReleaseTagPattern: "*" + zeroReleaseTagSuffix

codeReview: base.#codeReview & {
	github:      githubRepositoryURL
	gerrit:      gerritRepositoryURL
	"cue-unity": unityRepositoryURL
}

// protectedBranchPatterns is a list of glob patterns to match the protected
// git branches which are continuously used during development on Gerrit.
// This includes the default branch and release branches,
// but excludes any others like feature branches or short-lived branches.
// Note that ci/test is excluded as it is GitHub-only.
protectedBranchPatterns: [defaultBranch, releaseBranchPattern]

// isLatestLinux returns a GitHub expression that evaluates to true if the job
// is running on Linux with the latest version of Go. This expression is often
// used to run certain steps just once per CI workflow, to avoid duplicated
// work.
isLatestLinux: "(matrix.go-version == '\(latestStableGo)' && matrix.os == '\(linuxMachine)')"
