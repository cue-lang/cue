package x

import "strings"

data: _

output: strings.Join([
	for _, v in data.commits {
		"* \(v.node.messageHeadline) by @\(v.node.author.user.login) in \(v.node.oid)"
	},
], "\n")
