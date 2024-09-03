package base

// This file contains aspects principally related to git-codereview
// configuration.

import (
	"strings"
)

// #codeReview defines the schema of a codereview.cfg file that
// sits at the root of a repository. codereview.cfg is the configuration
// file that drives golang.org/x/review/git-codereview. This config
// file is also used by github.com/cue-lang/contrib-tools/cmd/cueckoo.
#codeReview: {
	gerrit?:      string
	github?:      string
	"cue-unity"?: string
}

// #toCodeReviewCfg converts a #codeReview instance to
// the key: value
toCodeReviewCfg: {
	#input: #codeReview
	let parts = [for k, v in #input {k + ": " + v}]

	// Per https://pkg.go.dev/golang.org/x/review/git-codereview#hdr-Configuration
	strings.Join(parts, "\n")
}
