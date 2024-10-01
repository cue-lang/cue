// package repo contains data values that are common to all CUE configurations
// in this repo. The list of configurations includes GitHub workflows, but also
// things like gerrit configuration etc.
package repo

import (
	"cuelang.org/go/internal/ci/base"
)

base

earlyChecks: run: "go run ./internal/ci/checks"

githubRepositoryPath: "cue-lang/cue"

unityRepositoryPath: "cue-unity/unity-private"
unityRepositoryURL:  "https://github.com/" + unityRepositoryPath

cuelangRepositoryPath: "cue-lang/cuelang.org"

defaultBranch:        _
releaseBranchPrefix:  "release-branch."
releaseBranchPattern: releaseBranchPrefix + "*"
protectedBranchPatterns: [defaultBranch, releaseBranchPattern]

botGitHubUser:      "cueckoo"
botGitHubUserEmail: "cueckoo@gmail.com"

linuxMachine:   "ubuntu-22.04"
macosMachine:   "macos-14"
windowsMachine: "windows-2022"

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
// This may be a release candidate if we are late into a Go release cycle.
latestGo: "1.23.x"

// The list of all Go versions that we run our tests on.
// This typically goes back one major Go version, as we support two at a time.
matrixGo: ["1.22.x", latestGo]

// Use a specific latest version for release builds.
// Note that we don't want ".x" for the sake of reproducibility,
// so we instead pin a specific Go release.
pinnedReleaseGo: "1.23.2"

goreleaserVersion: "v2.3.2"

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

codeReview: "cue-unity": unityRepositoryURL
