package base

// This file contains everything else

import (
	"list"
	"strings"
)

// _matchPattern returns a GitHub Actions expression which evaluates whether a
// variable matches a globbing pattern. For literal patterns it uses "==",
// and for suffix patterns it uses "startsWith".
// See https://docs.github.com/en/actions/learn-github-actions/expressions.
_matchPattern: {
	variable: string
	pattern:  string
	expr: [
		if strings.HasSuffix(pattern, "*") {
			let prefix = strings.TrimSuffix(pattern, "*")
			"startsWith(\(variable), '\(prefix)')"
		},
		{
			"\(variable) == '\(pattern)'"
		},
	][0]
}

doNotEditMessage: {
	#generatedBy: string
	"Code generated \(#generatedBy); DO NOT EDIT."
}

// #URLPath is a temporary measure to derive the path part of a URL.
//
// TODO: remove when cuelang.org/issue/1433 lands.
URLPath: {
	#url: string
	let parts = strings.Split(#url, "/")
	strings.Join(list.Slice(parts, 3, len(parts)), "/")
}
