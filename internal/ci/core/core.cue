// package core contains data values that are commont to all CUE
// configurations. This not only includes GitHub workflows, but also things
// like gerrit configuration etc.
package core

import (
	"list"
	"strings"
)

// Define core URLs that will be used in the codereview.cfg and GitHub workflows
#githubRepositoryURL:  "https://github.com/cue-lang/cue"
#gerritRepositoryURL:  "https://review.gerrithub.io/a/cue-lang/cue"
#githubRepositoryPath: _#URLPath & {#url: #githubRepositoryURL, _}
#unityRepositoryURL:   "https://github.com/cue-unity/unity"

// Not ideal, but hack together something that gives us the path
// of a URL. In lieu of cuelang.org/issue/1433
_#URLPath: {
	#url: string
	let parts = strings.Split(#url, "/")
	strings.Join(list.Slice(parts, 3, len(parts)), "/")
}

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
#latestStableGo: "1.19.x"

// Use a specific latest version for release builds.
// Note that we don't want ".x" for the sake of reproducibility,
// so we instead pin a specific Go release.
#pinnedReleaseGo: "1.19.7"

#goreleaserVersion: "v1.13.1"

#defaultBranch: "master"

// #releaseBranchPrefix is the git branch name prefix used to identify
// release branches.
#releaseBranchPrefix: "release-branch."

// #releaseBranchPattern is the GitHub pattern that corresponds to
// #releaseBranchPrefix.
#releaseBranchPattern: #releaseBranchPrefix + "*"

// #releaseTagPrefix is the prefix used to identify all git tag that correspond
// to semver releases
#releaseTagPrefix: "v"

// #releaseTagPattern is the GitHub glob pattern that corresponds to
// #releaseTagPrefix.
#releaseTagPattern: #releaseTagPrefix + "*"

// #zeroReleaseTagSuffix is the suffix used to identify all "zero" releases.
// When we create a release branch for v0.$X.0, it's likely that commits on the
// default branch will from that point onwards be intended for the $X+1
// version. However, unless we tag the next commit after the release branch, it
// might be the case that pseudo versions of those later commits refer to the
// $X release.
//
// A "zero" tag fixes this when applied to the first commit after a release
// branch. Critically, the -0.dev pre-release suffix is ordered before -alpha.
// tags.
#zeroReleaseTagSuffix: "-0.dev"

// #zeroReleaseTagPattern is the GitHub glob pattern that corresponds
// #zeroReleaseTagSuffix.
#zeroReleaseTagPattern: "*" + #zeroReleaseTagSuffix

#codeReview: {
	gerrit?:      string
	github?:      string
	"cue-unity"?: string
}

codeReview: #codeReview & {
	github:      #githubRepositoryURL
	gerrit:      #gerritRepositoryURL
	"cue-unity": #unityRepositoryURL
}

// #toCodeReviewCfg converts a #codeReview instance to
// the key: value
#toCodeReviewCfg: {
	#input: #codeReview
	let parts = [ for k, v in #input {k + ": " + v}]

	// Per https://pkg.go.dev/golang.org/x/review/git-codereview#hdr-Configuration
	strings.Join(parts, "\n")
}
