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

#defaultBranch:        "master"
#releaseBranchPattern: "release-branch.*"
#releaseTagPattern:    "v*"

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
